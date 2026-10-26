package public_port_routing

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/locks"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
	"testing"
)

type prerequisiteAPI struct{ recoveryAPI }

func (*prerequisiteAPI) ListPortRoutes(context.Context, string, client.PageQuery) (client.Page[client.PortRoute], error) {
	return client.Page[client.PortRoute]{}, nil
}
func (f *prerequisiteAPI) CreatePortRoute(_ context.Context, _ string, v client.PortRouteWrite) (client.PortRoute, error) {
	f.calls = append(f.calls, "create")
	if v.PublicIPType != "IP_V4" || v.FirewallAllowMyIP {
		panic("incorrect IPv4 serialization")
	}
	return client.PortRoute{}, &client.APIError{HTTPStatus: 400, Code: "publicIpTypeNotSupported"}
}
func TestIPv4CreatePrerequisiteNoSyncOrState(t *testing.T) {
	ctx := context.Background()
	api := &prerequisiteAPI{}
	r := &Resource{data: &providerdata.ProviderData{API: api, Locks: &locks.Keyed{}}}
	var sr resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sr)
	m := Model{ID: types.StringUnknown(), ServiceID: types.StringValue("service"), PublicIPType: types.StringValue("IP_V4"), PublicPort: types.Int64Value(23084), InternalPort: types.Int64Value(3000), Protocol: types.StringValue("tcp"), FirewallPolicy: types.StringValue("BLACKLIST"), FirewallIPRanges: types.SetValueMust(types.StringType, []attr.Value{}), IsSynced: types.BoolUnknown(), Timeouts: timeouts.Value{Object: types.ObjectNull(map[string]attr.Type{"create": types.StringType, "update": types.StringType, "delete": types.StringType})}}
	plan := tfsdk.Plan{Schema: sr.Schema}
	if d := plan.Set(ctx, &m); d.HasError() {
		t.Fatal(d)
	}
	resp := resource.CreateResponse{State: tfsdk.State{Schema: sr.Schema}}
	r.Create(ctx, resource.CreateRequest{Plan: plan}, &resp)
	if !resp.Diagnostics.HasError() || !resp.State.Raw.IsNull() {
		t.Fatalf("unsafe failed create: %v", resp.Diagnostics)
	}
	if len(api.calls) != 1 || api.calls[0] != "create" {
		t.Fatalf("prerequisite attempted sync/adoption: %v", api.calls)
	}
}
