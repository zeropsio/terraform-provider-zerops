package project_vpn_peer

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"testing"
)

func TestTimeoutOnlyUpdateDoesNotCallAPI(t *testing.T) {
	ctx := context.Background()
	r := &configResource{}
	var schema resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schema)
	timeoutTypes := map[string]attr.Type{"create": types.StringType, "update": types.StringType, "delete": types.StringType}
	m := model{ID: types.StringValue("p/key"), ProjectID: types.StringValue("p"), PublicKey: types.StringValue("key"), Timeouts: timeouts.Value{Object: types.ObjectNull(timeoutTypes)}, IPv4Address: types.StringValue("10.0.0.3"), IPv6Address: types.StringValue("fd00::3"), Setup: types.StringValue("setup"), SetupLinux: types.StringValue("linux"), SetupMacOS: types.StringValue("mac"), SetupWindows: types.StringValue("windows")}
	state := tfsdk.State{Schema: schema.Schema}
	if d := state.Set(ctx, &m); d.HasError() {
		t.Fatal(d)
	}
	m.Timeouts = timeouts.Value{Object: types.ObjectValueMust(timeoutTypes, map[string]attr.Value{"create": types.StringNull(), "update": types.StringValue("1m"), "delete": types.StringNull()})}
	planned := tfsdk.State{Schema: schema.Schema}
	if d := planned.Set(ctx, &m); d.HasError() {
		t.Fatal(d)
	}
	resp := resource.UpdateResponse{State: tfsdk.State{Schema: schema.Schema}}
	// With no ProviderData, any accidental API request would panic.
	r.Update(ctx, resource.UpdateRequest{State: state, Plan: tfsdk.Plan{Schema: schema.Schema, Raw: planned.Raw}}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	var got model
	if d := resp.State.Get(ctx, &got); d.HasError() || !got.Timeouts.Equal(m.Timeouts) || got.SetupLinux.ValueString() != "linux" {
		t.Fatal("state-only update lost timeouts/readbacks", d)
	}
}
