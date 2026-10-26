package public_http_routing

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"testing"
)

func TestModifyPlanOwnedUnsynced(t *testing.T) {
	ctx := context.Background()
	r := &Resource{}
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	typ := schemaResp.Schema.Type().TerraformType(ctx)
	object := typ.(tftypes.Object)
	vals := map[string]tftypes.Value{}
	for k, v := range object.AttributeTypes {
		vals[k] = tftypes.NewValue(v, nil)
	}
	vals["id"] = tftypes.NewValue(tftypes.String, "own")
	vals["is_synced"] = tftypes.NewValue(tftypes.Bool, false)
	raw := tftypes.NewValue(typ, vals)
	state := tfsdk.State{Schema: schemaResp.Schema, Raw: raw}
	plan := tfsdk.Plan{Schema: schemaResp.Schema, Raw: raw}
	resp := resource.ModifyPlanResponse{Plan: plan}
	r.ModifyPlan(ctx, resource.ModifyPlanRequest{State: state, Plan: plan}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	var synced types.Bool
	d := resp.Plan.GetAttribute(ctx, path.Root("is_synced"), &synced)
	if d.HasError() || !synced.ValueBool() {
		t.Fatalf("recovery not planned: %v %v", synced, d)
	}
	// Destroy must never be changed into an update.
	destroy := tfsdk.Plan{Schema: schemaResp.Schema, Raw: tftypes.NewValue(typ, nil)}
	resp = resource.ModifyPlanResponse{Plan: destroy}
	r.ModifyPlan(ctx, resource.ModifyPlanRequest{State: state, Plan: destroy}, &resp)
	if !resp.Plan.Raw.IsNull() {
		t.Fatal("destroy plan modified")
	}
}
