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
	"strings"
	"testing"
)

type recoveryAPI struct {
	client.API
	v       client.PortRoute
	calls   []string
	foreign bool
}

func (f *recoveryAPI) GetPortRoute(_ context.Context, _ string) (client.PortRoute, error) {
	f.calls = append(f.calls, "get")
	return f.v, nil
}
func (f *recoveryAPI) RestorePortRoute(_ context.Context, _ string) (client.PortRoute, error) {
	f.calls = append(f.calls, "restore")
	f.v.DeleteOnSync = false
	return f.v, nil
}
func (f *recoveryAPI) UpdatePortRoute(_ context.Context, _ string, w client.PortRouteWrite) (client.PortRoute, error) {
	f.calls = append(f.calls, "update")
	f.v.PortRouteWrite = w
	return f.v, nil
}
func (f *recoveryAPI) SyncPortRoutes(_ context.Context, _ string) (client.Process, error) {
	f.calls = append(f.calls, "sync")
	f.v.IsSynced = true
	return client.Process{ID: "sync"}, nil
}
func (f *recoveryAPI) GetProcess(_ context.Context, _ string) (client.Process, error) {
	return client.Process{ID: "sync", Status: "FINISHED"}, nil
}
func (f *recoveryAPI) GetService(_ context.Context, _ string) (client.Service, error) {
	return client.Service{ID: "service", ProjectID: "project"}, nil
}
func (f *recoveryAPI) ListServices(_ context.Context, _ string, _ client.ServiceQuery) (client.Page[client.Service], error) {
	return client.Page[client.Service]{Items: []client.Service{{ID: "service"}}, Total: 1}, nil
}
func (f *recoveryAPI) ListPortRoutes(_ context.Context, _ string, _ client.PageQuery) (client.Page[client.PortRoute], error) {
	v := []client.PortRoute{f.v}
	if f.foreign {
		v = append(v, client.PortRoute{ID: "foreign"})
	}
	return client.Page[client.PortRoute]{Items: v, Total: len(v)}, nil
}
func TestPendingDeleteRecoveryOrder(t *testing.T) {
	for _, ipType := range []string{"IP_V6", "IP_V4"} {
		t.Run(ipType, func(t *testing.T) {
			ctx := context.Background()
			v := client.PortRoute{ID: "own", ServiceStackID: "service", DeleteOnSync: true, PortRouteWrite: client.PortRouteWrite{PublicIPType: ipType, PublicPort: 20000, InternalPort: 3000, InternalProtocol: "tcp", FirewallPolicy: "BLACKLIST", FirewallIPRanges: []string{}}}
			api := &recoveryAPI{v: v}
			r := &Resource{data: &providerdata.ProviderData{API: api, Locks: &locks.Keyed{}}}
			var sr resource.SchemaResponse
			r.Schema(ctx, resource.SchemaRequest{}, &sr)
			var m Model
			if e := m.read(ctx, v); e != nil {
				t.Fatal(e)
			}
			m.ForceSync = types.BoolNull()
			m.Timeouts = timeouts.Value{Object: types.ObjectNull(map[string]attr.Type{"create": types.StringType, "update": types.StringType, "delete": types.StringType})}
			state := tfsdk.State{Schema: sr.Schema}
			if d := state.Set(ctx, &m); d.HasError() {
				t.Fatal(d)
			}
			readResp := resource.ReadResponse{State: state}
			r.Read(ctx, resource.ReadRequest{State: state}, &readResp)
			if readResp.Diagnostics.HasError() {
				t.Fatal(readResp.Diagnostics)
			}
			if strings.Join(api.calls, ",") != "get" {
				t.Fatalf("refresh mutated: %v", api.calls)
			}
			api.calls = nil
			m.IsSynced = types.BoolValue(true)
			plan := tfsdk.Plan{Schema: sr.Schema}
			if d := plan.Set(ctx, &m); d.HasError() {
				t.Fatal(d)
			}
			resp := resource.UpdateResponse{State: state}
			r.Update(ctx, resource.UpdateRequest{Plan: plan, State: state}, &resp)
			if resp.Diagnostics.HasError() {
				t.Fatal(resp.Diagnostics)
			}
			calls := strings.Join(api.calls, ",")
			if !strings.Contains(calls, "get,restore,get,update,sync,get") {
				t.Fatalf("unsafe restore/stage/sync order: %s", calls)
			}
			// The same pending own delete must not be restored if a foreign item blocks.
			api.v.DeleteOnSync = true
			api.foreign = true
			api.calls = nil
			resp = resource.UpdateResponse{State: state}
			r.Update(ctx, resource.UpdateRequest{Plan: plan, State: state}, &resp)
			if !resp.Diagnostics.HasError() {
				t.Fatal("foreign staging did not block")
			}
			for _, c := range api.calls {
				if c == "restore" || c == "update" || c == "sync" {
					t.Fatalf("mutated before gate: %v", api.calls)
				}
			}
		})
	}
}
