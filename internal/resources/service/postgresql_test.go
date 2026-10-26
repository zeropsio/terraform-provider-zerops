package service

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
)

type postgresqlAPI struct {
	client.API
	settings    client.Settings
	create      func(client.ServiceCreate)
	settingsErr error
}

func (a postgresqlAPI) Settings(context.Context, string) (client.Settings, error) {
	return a.settings, a.settingsErr
}

func (a postgresqlAPI) CreateService(_ context.Context, _ string, input client.ServiceCreate) (client.ServiceProcess, error) {
	a.create(input)
	return client.ServiceProcess{}, errors.New("stop after capturing create payload")
}

func pgVertical(min, max types.Number) types.Object {
	value, _ := ScalingValues(&client.Autoscaling{Vertical: &client.VerticalAutoscaling{MinResource: &client.ScalingResource{DiskGBytes: new(float64)}}}, true)
	attrs := value.Attributes()
	attrs["min_disk_gb"], attrs["max_disk_gb"] = min, max
	return types.ObjectValueMust(ScalingTypes(true), attrs)
}

func TestPostgresqlDiskFloorBounds(t *testing.T) {
	for _, tc := range []struct {
		name     string
		min, max types.Number
		bad      bool
	}{
		{"below-min", types.NumberValue(big.NewFloat(1)), types.NumberNull(), true},
		{"below-max", types.NumberNull(), types.NumberValue(big.NewFloat(1)), true},
		{"at-floor", types.NumberValue(big.NewFloat(2)), types.NumberValue(big.NewFloat(2)), false},
		{"unknown", types.NumberUnknown(), types.NumberUnknown(), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var diagnostics diag.Diagnostics
			postgresqlScaling(pgVertical(tc.min, tc.max), 2, nil, &diagnostics)
			if diagnostics.HasError() != tc.bad {
				t.Fatal(diagnostics)
			}
		})
	}
}

func TestPostgresqlDiskFloorDefaultPayload(t *testing.T) {
	for _, max := range []float64{1, 5} {
		defaults := &client.VerticalAutoscaling{MaxResource: &client.ScalingResource{DiskGBytes: &max}}
		for _, value := range []types.Object{nullScaling(true), pgVertical(types.NumberNull(), types.NumberNull()), pgVertical(types.NumberValue(big.NewFloat(3)), types.NumberNull())} {
			var diagnostics diag.Diagnostics
			planned := postgresqlScaling(value, 3, defaults, &diagnostics)
			payload, err := scalingInput(planned, nullScaling(false))
			if err != nil || diagnostics.HasError() || *payload.Vertical.MinResource.DiskGBytes != 3 {
				t.Fatalf("payload=%#v diagnostics=%v err=%v", payload, diagnostics, err)
			}
			gotMax := payload.Vertical.MaxResource.DiskGBytes
			if max < 3 && (gotMax == nil || *gotMax != 3) {
				t.Fatal("max not raised to floor")
			}
			if max >= 3 && gotMax != nil {
				t.Fatal("platform max unnecessarily overridden")
			}
		}
	}
}

func TestPostgresqlCatalogIdentity(t *testing.T) {
	for _, tc := range []struct {
		id, name, category, version, configured string
		want                                    bool
	}{
		{"postgresql", "Database", "MANAGED", "database@16", "database@16", true},
		{"db", "PostgreSQL", "MANAGED", "postgresql@16", "postgresql:single@16", true},
		{"db", "Database", "POSTGRESQL", "database@16", "database@16", true},
		{"nodejs", "Node.js", "RUNTIME", "nodejs@22", "nodejs@22", false},
		{"", "", "", "postgresql@16", "postgresql@16", true},
	} {
		settings := client.Settings{ServiceTypes: []client.ServiceType{{ID: tc.id, Name: tc.name, Category: tc.category, Versions: []client.ServiceTypeVersion{{Name: tc.version}}}}}
		got, _ := catalogService(settings, tc.configured)
		if got != tc.want {
			t.Fatalf("%+v got=%t", tc, got)
		}
	}
}

func TestPostgresqlModifyPlan(t *testing.T) {
	ctx := context.Background()
	r := &Resource{data: &providerdata.ProviderData{API: postgresqlAPI{}, PostgresqlMinDiskGB: 3}}
	var schema resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schema)
	for _, tc := range []struct {
		name, typ string
		vertical  types.Object
		wantError bool
	}{
		{"custom-floor", "postgresql:single@16", pgVertical(types.NumberValue(big.NewFloat(2)), types.NumberNull()), true},
		{"unknown", "postgresql@16", pgVertical(types.NumberUnknown(), types.NumberUnknown()), false},
		{"default", "postgresql@16", nullScaling(true), false},
		{"non-postgresql-null", "nodejs@22", nullScaling(true), false},
		{"non-postgresql-explicit", "nodejs@22", pgVertical(types.NumberValue(big.NewFloat(1)), types.NumberValue(big.NewFloat(1))), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Set a full framework model so planning exercises the real nested schema.
			model := Model{Type: types.StringValue(tc.typ), Vertical: tc.vertical, Horizontal: nullScaling(false), Current: CurrentValue(nil), Timeouts: timeouts.Value{Object: types.ObjectNull(map[string]attr.Type{"create": types.StringType, "update": types.StringType, "delete": types.StringType})}}
			plan := tfsdk.Plan{Schema: schema.Schema}
			if d := plan.Set(ctx, &model); d.HasError() {
				t.Fatal(d)
			}
			req := resource.ModifyPlanRequest{Plan: plan, Config: tfsdk.Config{Schema: schema.Schema, Raw: plan.Raw}}
			resp := resource.ModifyPlanResponse{Plan: plan}
			r.ModifyPlan(ctx, req, &resp)
			if resp.Diagnostics.HasError() != tc.wantError {
				t.Fatal(resp.Diagnostics)
			}
			if !tc.wantError && tc.name != "unknown" {
				var captured *client.Autoscaling
				r.data.API = postgresqlAPI{create: func(input client.ServiceCreate) { captured = input.CustomAutoscaling }}
				var created resource.CreateResponse
				r.Create(ctx, resource.CreateRequest{Plan: resp.Plan}, &created)
				if captured == nil {
					t.Fatalf("create payload not captured: %v", created.Diagnostics)
				}
				if tc.name == "default" && (captured.Vertical == nil || *captured.Vertical.MinResource.DiskGBytes != 3) {
					t.Fatal("PostgreSQL create did not receive provider floor")
				}
				if tc.name == "non-postgresql-null" && captured.Vertical != nil {
					t.Fatal("non-PostgreSQL create changed")
				}
				if tc.name == "non-postgresql-explicit" && *captured.Vertical.MinResource.DiskGBytes != 1 {
					t.Fatal("non-PostgreSQL create disk changed")
				}
			}
			if tc.typ == "nodejs@22" {
				var got types.Object
				resp.Plan.GetAttribute(ctx, path.Root("vertical_autoscaling"), &got)
				if !got.Equal(tc.vertical) {
					t.Fatal("non-PostgreSQL plan changed")
				}
				payload, err := scalingInput(got, nullScaling(false))
				if err != nil {
					t.Fatal(err)
				}
				if tc.vertical.IsNull() && payload.Vertical != nil {
					t.Fatal("non-PostgreSQL payload changed")
				}
				if !tc.vertical.IsNull() && *payload.Vertical.MinResource.DiskGBytes != 1 {
					t.Fatal("non-PostgreSQL disk changed")
				}
			}
		})
	}
	// Unconfigured providers and destroy plans must not consult the API.
	(&Resource{}).ModifyPlan(ctx, resource.ModifyPlanRequest{}, &resource.ModifyPlanResponse{})
	r.ModifyPlan(ctx, resource.ModifyPlanRequest{Plan: tfsdk.Plan{Schema: schema.Schema, Raw: tftypes.NewValue(schema.Schema.Type().TerraformType(ctx), nil)}}, &resource.ModifyPlanResponse{})
}

func TestPostgresqlExplicitMinimumRaisesInheritedMaximum(t *testing.T) {
	platformMax := 1.0
	defaults := &client.VerticalAutoscaling{MaxResource: &client.ScalingResource{DiskGBytes: &platformMax}}
	for _, min := range []float64{3, 5} {
		var diagnostics diag.Diagnostics
		planned := postgresqlScaling(pgVertical(nval(&min, false), types.NumberNull()), 3, defaults, &diagnostics)
		payload, err := scalingInput(planned, nullScaling(false))
		if err != nil || diagnostics.HasError() || payload.Vertical.MaxResource.DiskGBytes == nil || *payload.Vertical.MaxResource.DiskGBytes != min {
			t.Fatalf("explicit minimum %g did not raise inherited maximum: %+v %v %v", min, payload, diagnostics, err)
		}
	}
}

func TestPostgresqlFractionalFloorRoundTrip(t *testing.T) {
	var diagnostics diag.Diagnostics
	planned := postgresqlScaling(nullScaling(true), 2.125, nil, &diagnostics)
	payload, err := scalingInput(planned, nullScaling(false))
	if err != nil || diagnostics.HasError() {
		t.Fatalf("%v %v", err, diagnostics)
	}
	if got := scalingMismatch(context.Background(), planned, nullScaling(false), payload); got.HasError() {
		t.Fatal(got)
	}
	readback, _ := ScalingValues(payload, true)
	if !readback.Equal(planned) {
		t.Fatal("fractional floor changed during readback")
	}
}

func TestPostgresqlUnknownTypeResolvesWithoutKnownNullDefaults(t *testing.T) {
	ctx := context.Background()
	r := &Resource{data: &providerdata.ProviderData{API: postgresqlAPI{}, PostgresqlMinDiskGB: 2}}
	var schema resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schema)
	for _, nested := range []bool{false, true} {
		configured, proposed := nullScaling(true), types.ObjectUnknown(ScalingTypes(true))
		if nested {
			configured = pgVertical(types.NumberNull(), types.NumberNull())
			attrs := configured.Attributes()
			attrs["swap_enabled"] = types.BoolValue(false)
			configured = types.ObjectValueMust(ScalingTypes(true), attrs)
			attrs = configured.Attributes()
			attrs["min_disk_gb"], attrs["max_disk_gb"] = types.NumberUnknown(), types.NumberUnknown()
			proposed = types.ObjectValueMust(ScalingTypes(true), attrs)
		}
		model := Model{Type: types.StringUnknown(), Vertical: configured, Horizontal: nullScaling(false), Current: CurrentValue(nil), Timeouts: timeouts.Value{Object: types.ObjectNull(map[string]attr.Type{"create": types.StringType, "update": types.StringType, "delete": types.StringType})}}
		configPlan := tfsdk.Plan{Schema: schema.Schema}
		if d := configPlan.Set(ctx, &model); d.HasError() {
			t.Fatal(d)
		}
		config := tfsdk.Config{Schema: schema.Schema, Raw: configPlan.Raw}
		model.Vertical = proposed
		plan := tfsdk.Plan{Schema: schema.Schema}
		if d := plan.Set(ctx, &model); d.HasError() {
			t.Fatal(d)
		}
		resp := resource.ModifyPlanResponse{Plan: plan}
		r.ModifyPlan(ctx, resource.ModifyPlanRequest{Config: config, Plan: plan}, &resp)
		if resp.Diagnostics.HasError() || !resp.Plan.Raw.Equal(plan.Raw) {
			t.Fatalf("unknown type lost computed defaults: %v", resp.Diagnostics)
		}

		// Apply-time replanning resolves the type while the original disk plan
		// remains unknown, permitting the now-known PostgreSQL default.
		model.Type = types.StringValue("postgresql:single@16")
		if d := plan.Set(ctx, &model); d.HasError() {
			t.Fatal(d)
		}
		resp = resource.ModifyPlanResponse{Plan: plan}
		r.ModifyPlan(ctx, resource.ModifyPlanRequest{Config: config, Plan: plan}, &resp)
		var actual types.Object
		resp.Diagnostics.Append(resp.Plan.GetAttribute(ctx, path.Root("vertical_autoscaling"), &actual)...)
		if resp.Diagnostics.HasError() || !actual.Attributes()["min_disk_gb"].Equal(types.NumberValue(big.NewFloat(2))) {
			t.Fatalf("resolved PostgreSQL floor missing: %v", resp.Diagnostics)
		}
	}
}

func TestNonPostgresqlCatalogFailurePreservesPlan(t *testing.T) {
	ctx := context.Background()
	r := &Resource{data: &providerdata.ProviderData{API: postgresqlAPI{settingsErr: errors.New("catalog unavailable")}, PostgresqlMinDiskGB: 2}}
	var schema resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schema)
	for _, vertical := range []types.Object{nullScaling(true), pgVertical(types.NumberValue(big.NewFloat(1)), types.NumberValue(big.NewFloat(1)))} {
		model := Model{Type: types.StringValue("nodejs@22"), Vertical: vertical, Horizontal: nullScaling(false), Current: CurrentValue(nil), Timeouts: timeouts.Value{Object: types.ObjectNull(map[string]attr.Type{"create": types.StringType, "update": types.StringType, "delete": types.StringType})}}
		plan := tfsdk.Plan{Schema: schema.Schema}
		if d := plan.Set(ctx, &model); d.HasError() {
			t.Fatal(d)
		}
		resp := resource.ModifyPlanResponse{Plan: plan}
		r.ModifyPlan(ctx, resource.ModifyPlanRequest{Plan: plan, Config: tfsdk.Config{Schema: schema.Schema, Raw: plan.Raw}}, &resp)
		if resp.Diagnostics.HasError() || !resp.Plan.Raw.Equal(plan.Raw) {
			t.Fatalf("catalog outage changed non-PostgreSQL planning: %v", resp.Diagnostics)
		}
	}
}

func TestPostgresqlCatalogFailureFailsClosed(t *testing.T) {
	ctx := context.Background()
	r := &Resource{data: &providerdata.ProviderData{API: postgresqlAPI{settingsErr: errors.New("catalog unavailable")}, PostgresqlMinDiskGB: 3}}
	var schema resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schema)
	model := Model{Type: types.StringValue("postgresql@16"), Vertical: nullScaling(true), Horizontal: nullScaling(false), Current: CurrentValue(nil), Timeouts: timeouts.Value{Object: types.ObjectNull(map[string]attr.Type{"create": types.StringType, "update": types.StringType, "delete": types.StringType})}}
	plan := tfsdk.Plan{Schema: schema.Schema}
	if d := plan.Set(ctx, &model); d.HasError() {
		t.Fatal(d)
	}
	resp := resource.ModifyPlanResponse{Plan: plan}
	r.ModifyPlan(ctx, resource.ModifyPlanRequest{Plan: plan, Config: tfsdk.Config{Schema: schema.Schema, Raw: plan.Raw}}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("PostgreSQL planned without catalog default bounds")
	}
}
