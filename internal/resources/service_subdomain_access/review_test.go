package service_subdomain_access

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"testing"
)

func TestTimeoutPlanKeepsComputedState(t *testing.T) {
	ctx := context.Background()
	r := accessResource{}
	var sr resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sr)
	old := model{ID: types.StringValue("s"), ServiceID: types.StringValue("s"), URLs: types.SetValueMust(types.StringType, []attr.Value{types.StringValue("https://app.example")}), Timeouts: timeouts.Value{Object: types.ObjectNull(map[string]attr.Type{"create": types.StringType, "delete": types.StringType})}}
	state := tfsdk.State{Schema: sr.Schema}
	if d := state.Set(ctx, old); d.HasError() {
		t.Fatal(d)
	}
	plan := tfsdk.Plan{Schema: sr.Schema, Raw: state.Raw}
	id := sr.Schema.Attributes["id"].(schema.StringAttribute)
	urls := sr.Schema.Attributes["urls"].(schema.SetAttribute)
	if len(id.PlanModifiers) == 0 || len(urls.PlanModifiers) == 0 {
		t.Fatal("missing computed state modifiers")
	}
	ir := planmodifier.StringResponse{PlanValue: types.StringUnknown()}
	id.PlanModifiers[0].PlanModifyString(ctx, planmodifier.StringRequest{State: state, Plan: plan, ConfigValue: types.StringNull(), PlanValue: types.StringUnknown(), StateValue: old.ID}, &ir)
	ur := planmodifier.SetResponse{PlanValue: types.SetUnknown(types.StringType)}
	urls.PlanModifiers[0].PlanModifySet(ctx, planmodifier.SetRequest{State: state, Plan: plan, ConfigValue: types.SetNull(types.StringType), PlanValue: types.SetUnknown(types.StringType), StateValue: old.URLs}, &ur)
	if !ir.PlanValue.Equal(old.ID) || !ur.PlanValue.Equal(old.URLs) {
		t.Fatalf("computed values not retained: %v %v", ir.PlanValue, ur.PlanValue)
	}
}
