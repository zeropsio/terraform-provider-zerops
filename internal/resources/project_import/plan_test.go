package project_import

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
	"testing"
)

func TestOrganizationPlanFallbackAndUnknown(t *testing.T) {
	for _, configured := range []types.String{types.StringNull(), types.StringUnknown(), types.StringValue("explicit")} {
		ctx := context.Background()
		r := Resource{data: &providerdata.ProviderData{OrganizationID: "provider-org"}}
		var sr resource.SchemaResponse
		r.Schema(ctx, resource.SchemaRequest{}, &sr)
		m := Model{OrganizationID: configured, YAML: types.StringValue("project: {name: name}\nservices: []"), Services: types.MapUnknown(types.StringType), Timeouts: timeouts.Value{Object: types.ObjectNull(map[string]attr.Type{"create": types.StringType, "update": types.StringType, "delete": types.StringType})}}
		config := tfsdk.Config{Schema: sr.Schema}
		plan := tfsdk.Plan{Schema: sr.Schema}
		state := tfsdk.State{Schema: sr.Schema}
		// Config and plan share the schema's Terraform value representation.
		source := tfsdk.Plan{Schema: sr.Schema}
		if d := source.Set(ctx, &m); d.HasError() {
			t.Fatal(d)
		}
		config.Raw = source.Raw
		if configured.IsNull() {
			m.OrganizationID = types.StringUnknown()
		}
		if d := plan.Set(ctx, &m); d.HasError() {
			t.Fatal(d)
		}
		response := resource.ModifyPlanResponse{Plan: plan}
		r.ModifyPlan(ctx, resource.ModifyPlanRequest{Config: config, Plan: plan, State: state}, &response)
		if response.Diagnostics.HasError() {
			t.Fatal(response.Diagnostics)
		}
		var got Model
		response.Plan.Get(ctx, &got)
		if configured.IsNull() {
			if got.OrganizationID.ValueString() != "provider-org" {
				t.Fatal("fallback missing")
			}
		} else if !got.OrganizationID.Equal(configured) {
			t.Fatal("explicit/unknown organization changed")
		}
	}
}
