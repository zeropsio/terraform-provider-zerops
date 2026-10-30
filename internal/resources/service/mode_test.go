package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
)

func modeCatalog() client.Settings {
	var versions []client.ServiceTypeVersion
	for _, name := range []string{"postgresql:single@16", "postgresql:ha@16", "newdb:single@1", "newdb:ha@2", "meilisearch:single@1.44", "mysql:ha@5.7"} {
		versions = append(versions, client.ServiceTypeVersion{Name: name})
	}
	return client.Settings{ServiceTypes: []client.ServiceType{{Versions: versions}}}
}

func modeModel(typ types.String) Model {
	return Model{Type: typ, Vertical: nullScaling(true), Horizontal: nullScaling(false), Current: CurrentValue(nil), Timeouts: timeouts.Value{Object: types.ObjectNull(map[string]attr.Type{"create": types.StringType, "update": types.StringType, "delete": types.StringType})}}
}

func TestServiceModeModifyPlan(t *testing.T) {
	for _, tc := range []struct {
		name             string
		typ              types.String
		unavailable, bad bool
		options          []string
	}{
		{name: "shorthand", typ: types.StringValue("postgresql@16"), bad: true, options: []string{"postgresql:single@16", "postgresql:ha@16"}},
		{name: "case", typ: types.StringValue("PostgreSQL@16"), bad: true, options: []string{"PostgreSQL:single@16", "PostgreSQL:ha@16"}},
		{name: "single", typ: types.StringValue("postgresql:single@16")},
		{name: "ha", typ: types.StringValue("postgresql:ha@16")},
		{name: "runtime", typ: types.StringValue("nodejs@22")},
		{name: "qualified-runtime", typ: types.StringValue("alpine/nodejs@22")},
		{name: "unknown", typ: types.StringUnknown()},
		{name: "null", typ: types.StringNull()},
		{name: "single-only", typ: types.StringValue("meilisearch@1.44")},
		{name: "ha-only", typ: types.StringValue("mysql@5.7")},
		{name: "catalog-only", typ: types.StringValue("newdb@1"), bad: true, options: []string{"newdb:single@1", "newdb:ha@1"}},
		{name: "fallback", typ: types.StringValue("valkey@7.2"), unavailable: true, bad: true, options: []string{"valkey:single@7.2", "valkey:ha@7.2"}},
		{name: "fallback-runtime", typ: types.StringValue("nodejs@22"), unavailable: true},
		{name: "fallback-no-newdb", typ: types.StringValue("newdb@1"), unavailable: true},
		{name: "qualified-pg-fails-closed", typ: types.StringValue("postgresql:ha@16"), unavailable: true, bad: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			api := postgresqlAPI{settings: modeCatalog()}
			if tc.unavailable {
				api.settingsErr = errors.New("catalogue unavailable")
			}
			r := &Resource{data: &providerdata.ProviderData{API: api}}
			var sr resource.SchemaResponse
			r.Schema(ctx, resource.SchemaRequest{}, &sr)
			plan := tfsdk.Plan{Schema: sr.Schema}
			m := modeModel(tc.typ)
			if d := plan.Set(ctx, &m); d.HasError() {
				t.Fatal(d)
			}
			resp := resource.ModifyPlanResponse{Plan: plan}
			r.ModifyPlan(ctx, resource.ModifyPlanRequest{Plan: plan, Config: tfsdk.Config{Schema: sr.Schema, Raw: plan.Raw}}, &resp)
			if resp.Diagnostics.HasError() != tc.bad {
				t.Fatal(resp.Diagnostics)
			}
			if !tc.bad && len(resp.Diagnostics) != 0 {
				t.Fatal(resp.Diagnostics)
			}
			for _, option := range tc.options {
				if len(resp.Diagnostics) != 1 || !strings.Contains(resp.Diagnostics[0].Detail(), option) {
					t.Fatalf("missing %s: %v", option, resp.Diagnostics)
				}
			}
		})
	}
	ctx := context.Background()
	r := &Resource{data: &providerdata.ProviderData{API: postgresqlAPI{}}}
	var sr resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sr)
	resp := resource.ModifyPlanResponse{}
	r.ModifyPlan(ctx, resource.ModifyPlanRequest{Plan: tfsdk.Plan{Schema: sr.Schema, Raw: tftypes.NewValue(sr.Schema.Type().TerraformType(ctx), nil)}}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
}

func TestServiceTypeMigrationModifier(t *testing.T) {
	for _, tc := range []struct {
		old, next, base string
		replace         bool
	}{
		{"postgresql@16", "postgresql:ha@16", "postgresql:ha@16", false},
		{"postgresql@16", "PostgreSQL:HA@16", "postgresql:ha@16", false},
		{"postgresql@16", "postgresql:single@16", "postgresql:single@16", false},
		{"postgresql@16", "postgresql:single@16", "postgresql:ha@16", true},
		{"postgresql:ha@16", "postgresql@16", "postgresql:ha@16", true},
		{"postgresql@16", "postgresql:ha@17", "postgresql:ha@17", true},
		{"postgresql@16", "valkey:ha@16", "valkey:ha@16", true},
		{"nodejs@22", "alpine/nodejs@22", "alpine/nodejs@22", true},
	} {
		t.Run(tc.old+"-"+tc.next+"-"+tc.base, func(t *testing.T) {
			ctx := context.Background()
			r := &Resource{}
			var sr resource.SchemaResponse
			r.Schema(ctx, resource.SchemaRequest{}, &sr)
			old := modeModel(types.StringValue(tc.old))
			old.Base = types.StringValue(tc.base)
			state := tfsdk.State{Schema: sr.Schema}
			if d := state.Set(ctx, &old); d.HasError() {
				t.Fatal(d)
			}
			next := old
			next.Type = types.StringValue(tc.next)
			plan := tfsdk.Plan{Schema: sr.Schema}
			if d := plan.Set(ctx, &next); d.HasError() {
				t.Fatal(d)
			}
			resp := planmodifier.StringResponse{}
			serviceTypeRequiresReplace().PlanModifyString(ctx, planmodifier.StringRequest{Path: path.Root("type"), State: state, Plan: plan, StateValue: old.Type, PlanValue: next.Type}, &resp)
			if resp.Diagnostics.HasError() || resp.RequiresReplace != tc.replace {
				t.Fatalf("replace=%t diagnostics=%v", resp.RequiresReplace, resp.Diagnostics)
			}
			if !tc.replace {
				// No configured API or locks: any API use would panic.
				next.Current = types.ObjectUnknown(old.Current.AttributeTypes(ctx))
				next.Status = types.StringUnknown()
				if d := plan.Set(ctx, &next); d.HasError() {
					t.Fatal(d)
				}
				updated := resource.UpdateResponse{State: state}
				r.Update(ctx, resource.UpdateRequest{Plan: plan, State: state}, &updated)
				if updated.Diagnostics.HasError() {
					t.Fatal(updated.Diagnostics)
				}
				var got types.String
				if d := updated.State.GetAttribute(ctx, path.Root("type"), &got); d.HasError() {
					t.Fatal(d)
				}
				if !got.Equal(next.Type) {
					t.Fatalf("type not persisted: %v", got)
				}
				var current types.Object
				if d := updated.State.GetAttribute(ctx, path.Root("current_autoscaling"), &current); d.HasError() {
					t.Fatal(d)
				}
				if !current.Equal(old.Current) {
					t.Fatal("migration lost observed autoscaling")
				}
			}
		})
	}
}
