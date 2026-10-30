package service

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
)

func ramVertical(minimum, maximum types.Number) types.Object {
	value := pgVertical(types.NumberNull(), types.NumberNull())
	attrs := value.Attributes()
	attrs["min_ram_gb"], attrs["max_ram_gb"] = minimum, maximum
	return types.ObjectValueMust(ScalingTypes(true), attrs)
}

func TestManagedRAMFloorPlan(t *testing.T) {
	for _, family := range []struct {
		name, display, env string
		floor, catalogMax  float64
	}{
		{"clickhouse", "ClickHouse", "ZEROPS_CLICKHOUSE_MIN_RAM_GB", 0.375, 0.25},
		{"elasticsearch", "Elasticsearch", "ZEROPS_ELASTICSEARCH_MIN_RAM_GB", 2, 0.75},
	} {
		for _, mode := range []string{"single", "ha"} {
			for _, tc := range []struct {
				name                string
				vertical            types.Object
				override            float64
				badKey              string
				wantMax, unknownMax bool
			}{
				{name: "unset", vertical: nullScaling(true), wantMax: true},
				{name: "min-below", vertical: ramVertical(nval(&family.catalogMax, false), types.NumberNull()), badKey: "min_ram_gb"},
				{name: "max-below", vertical: ramVertical(types.NumberNull(), nval(&family.catalogMax, false)), badKey: "max_ram_gb"},
				{name: "override", vertical: nullScaling(true), override: family.floor * 2, wantMax: true},
				{name: "explicit-min", vertical: ramVertical(types.NumberValue(big.NewFloat(4)), types.NumberNull()), wantMax: true},
				{name: "unknown-min", vertical: ramVertical(types.NumberUnknown(), types.NumberNull()), unknownMax: true},
				{name: "unknown-object", vertical: types.ObjectUnknown(ScalingTypes(true))},
			} {
				t.Run(family.name+"/"+mode+"/"+tc.name, func(t *testing.T) {
					ctx := context.Background()
					name := family.name + ":" + mode + "@1"
					settings := client.Settings{ServiceTypes: []client.ServiceType{{ID: family.name, Versions: []client.ServiceTypeVersion{{Name: name, Config: client.ServiceTypeConfig{DefaultAutoscaling: &client.Autoscaling{Vertical: &client.VerticalAutoscaling{MaxResource: &client.ScalingResource{MemoryGBytes: &family.catalogMax}}}}}}}}}
					data := &providerdata.ProviderData{API: postgresqlAPI{settings: settings}}
					if family.name == "clickhouse" {
						data.ClickhouseMinRAMGB = tc.override
					} else {
						data.ElasticsearchMinRAMGB = tc.override
					}
					r := &Resource{data: data}
					var sr resource.SchemaResponse
					r.Schema(ctx, resource.SchemaRequest{}, &sr)
					m := modeModel(types.StringValue(name))
					m.Vertical = tc.vertical
					plan := tfsdk.Plan{Schema: sr.Schema}
					if d := plan.Set(ctx, &m); d.HasError() {
						t.Fatal(d)
					}
					response := resource.ModifyPlanResponse{Plan: plan}
					r.ModifyPlan(ctx, resource.ModifyPlanRequest{Plan: plan, Config: tfsdk.Config{Schema: sr.Schema, Raw: plan.Raw}}, &response)
					if response.Diagnostics.HasError() != (tc.badKey != "") {
						t.Fatal(response.Diagnostics)
					}
					if tc.badKey != "" {
						if len(response.Diagnostics) != 1 {
							t.Fatal(response.Diagnostics)
						}
						d := response.Diagnostics[0]
						if !strings.Contains(d.Detail(), family.env) || !strings.Contains(d.Detail(), family.display) {
							t.Fatal(d)
						}
						if withPath, ok := d.(diag.DiagnosticWithPath); !ok || !withPath.Path().Equal(path.Root("vertical_autoscaling").AtName(tc.badKey)) {
							t.Fatal(d)
						}
						return
					}
					var got types.Object
					if d := response.Plan.GetAttribute(ctx, path.Root("vertical_autoscaling"), &got); d.HasError() {
						t.Fatal(d)
					}
					if tc.vertical.IsUnknown() {
						if !got.IsUnknown() {
							t.Fatal(got)
						}
						return
					}
					attrs := got.Attributes()
					if tc.unknownMax {
						if !attrs["max_ram_gb"].IsUnknown() {
							t.Fatal(got)
						}
						return
					}
					floor := family.floor
					if tc.override != 0 {
						floor = tc.override
					}
					if tc.name == "explicit-min" {
						floor = 4
					}
					want := nval(&floor, false)
					if !attrs["min_ram_gb"].Equal(want) || (tc.wantMax && !attrs["max_ram_gb"].Equal(want)) {
						t.Fatal(got)
					}
					if !attrs["min_disk_gb"].IsNull() {
						t.Fatal("RAM floor changed disk")
					}
				})
			}
		}
	}
}

func TestManagedFloorCatalogIdentity(t *testing.T) {
	for _, family := range []string{"clickhouse", "elasticsearch"} {
		for _, identity := range []struct{ id, name, category, version string }{
			{id: family, name: "Database", version: "database@1"},
			{name: family, version: "database@1"},
			{category: family, version: "database@1"},
			{version: family + ":ha@1"},
		} {
			settings := client.Settings{ServiceTypes: []client.ServiceType{{ID: identity.id, Name: identity.name, Category: identity.category, Versions: []client.ServiceTypeVersion{{Name: identity.version}}}}}
			if matched, _ := catalogFamily(settings, identity.version, family); !matched {
				t.Fatalf("identity not matched: %+v", identity)
			}
		}
	}
}

func TestManagedFloorCatalogFailureAndOtherFamilies(t *testing.T) {
	for _, name := range []string{"clickhouse:single@1", "clickhouse:ha@1", "elasticsearch:single@1", "elasticsearch:ha@1", "nodejs@22", "valkey:single@7.2"} {
		for _, unavailable := range []bool{false, true} {
			t.Run(name+"/"+map[bool]string{true: "unavailable", false: "available"}[unavailable], func(t *testing.T) {
				ctx := context.Background()
				api := postgresqlAPI{}
				if unavailable {
					api.settingsErr = errors.New("catalogue unavailable")
				}
				r := &Resource{data: &providerdata.ProviderData{API: api}}
				var sr resource.SchemaResponse
				r.Schema(ctx, resource.SchemaRequest{}, &sr)
				m := modeModel(types.StringValue(name))
				plan := tfsdk.Plan{Schema: sr.Schema}
				if d := plan.Set(ctx, &m); d.HasError() {
					t.Fatal(d)
				}
				response := resource.ModifyPlanResponse{Plan: plan}
				r.ModifyPlan(ctx, resource.ModifyPlanRequest{Plan: plan, Config: tfsdk.Config{Schema: sr.Schema, Raw: plan.Raw}}, &response)
				floorFamily := strings.HasPrefix(name, "clickhouse:") || strings.HasPrefix(name, "elasticsearch:")
				if response.Diagnostics.HasError() != (unavailable && floorFamily) {
					t.Fatal(response.Diagnostics)
				}
				if !floorFamily && !response.Plan.Raw.Equal(plan.Raw) {
					t.Fatal("non-floor family changed")
				}
			})
		}
	}
}
