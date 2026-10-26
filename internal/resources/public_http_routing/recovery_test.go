package public_http_routing

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
	v       client.HTTPRoute
	calls   []string
	foreign bool
}

func (f *recoveryAPI) GetHTTPRoute(_ context.Context, _ string) (client.HTTPRoute, error) {
	f.calls = append(f.calls, "get")
	return f.v, nil
}
func (f *recoveryAPI) RestoreHTTPRoute(_ context.Context, _ string) (client.HTTPRoute, error) {
	f.calls = append(f.calls, "restore")
	f.v.DeleteOnSync = false
	return f.v, nil
}
func (f *recoveryAPI) UpdateHTTPRoute(_ context.Context, _ string, w client.HTTPRouteWrite) (client.HTTPRoute, error) {
	f.calls = append(f.calls, "update")
	f.v.SSLEnabled = w.SSLEnabled
	f.v.CDNEnabled = *w.CDNEnabled
	f.v.Locations = w.Locations
	f.v.Domains = nil
	for _, d := range w.Domains {
		f.v.Domains = append(f.v.Domains, client.HTTPDomain{DomainName: d})
	}
	return f.v, nil
}
func (f *recoveryAPI) SetHTTPLocations(_ context.Context, _ string, locations []client.HTTPLocation) (client.HTTPRoute, error) {
	f.calls = append(f.calls, "locations")
	f.v.Locations = locations
	return f.v, nil
}
func (f *recoveryAPI) SyncHTTPRoutes(_ context.Context, _ string) (client.Process, error) {
	f.calls = append(f.calls, "sync")
	f.v.IsSynced = true
	return client.Process{ID: "sync"}, nil
}
func (f *recoveryAPI) GetProcess(_ context.Context, _ string) (client.Process, error) {
	return client.Process{ID: "sync", Status: "FINISHED"}, nil
}
func (f *recoveryAPI) ListHTTPRoutes(_ context.Context, _ string, _ client.PageQuery) (client.Page[client.HTTPRoute], error) {
	v := []client.HTTPRoute{f.v}
	if f.foreign {
		v = append(v, client.HTTPRoute{ID: "foreign"})
	}
	return client.Page[client.HTTPRoute]{Items: v, Total: len(v)}, nil
}
func TestPendingDeleteRecoveryOrder(t *testing.T) {
	ctx := context.Background()
	v := client.HTTPRoute{ID: "own", ProjectID: "project", IsEditable: true, DeleteOnSync: true, Domains: []client.HTTPDomain{{DomainName: "a.example.com"}}, Locations: []client.HTTPLocation{{Path: "/", Port: 3000, ServiceStackID: "service"}}}
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
	if !strings.Contains(calls, "get,restore,get,update,locations,sync,get") {
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
}

func TestForceSyncPolicyUpdates(t *testing.T) {
	for _, tc := range []struct {
		name          string
		synced        bool
		force         types.Bool
		providerForce bool
		wantError     bool
		wantCalls     string
	}{
		{name: "synced explicit refusal", synced: true, force: types.BoolValue(false), providerForce: true, wantCalls: "get"},
		{name: "synced explicit force", synced: true, force: types.BoolValue(true), wantCalls: "get"},
		{name: "staged explicit refusal", force: types.BoolValue(false), providerForce: true, wantError: true, wantCalls: "get"},
		{name: "staged inherited refusal", force: types.BoolNull(), wantError: true, wantCalls: "get"},
		{name: "staged explicit force", force: types.BoolValue(true), wantCalls: "get,update,locations,sync,get"},
		{name: "staged inherited force", force: types.BoolNull(), providerForce: true, wantCalls: "get,update,locations,sync,get"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			api := &recoveryAPI{foreign: true, v: client.HTTPRoute{ID: "own", ProjectID: "project", IsEditable: true, IsSynced: tc.synced, Domains: []client.HTTPDomain{{DomainName: "a.example.com"}}, Locations: []client.HTTPLocation{{Path: "/", Port: 3000, ServiceStackID: "service"}}}}
			r := &Resource{data: &providerdata.ProviderData{API: api, Locks: &locks.Keyed{}, RoutingForceSync: tc.providerForce}}
			var sr resource.SchemaResponse
			r.Schema(ctx, resource.SchemaRequest{}, &sr)
			var m Model
			if err := m.read(ctx, api.v); err != nil {
				t.Fatal(err)
			}
			m.ForceSync = types.BoolNull()
			m.WaitForSSL = types.BoolValue(false)
			m.Timeouts = timeouts.Value{Object: types.ObjectNull(map[string]attr.Type{"create": types.StringType, "update": types.StringType, "delete": types.StringType})}
			state := tfsdk.State{Schema: sr.Schema}
			if d := state.Set(ctx, &m); d.HasError() {
				t.Fatal(d)
			}
			m.ForceSync = tc.force
			m.IsSynced = types.BoolValue(true)
			plan := tfsdk.Plan{Schema: sr.Schema}
			if d := plan.Set(ctx, &m); d.HasError() {
				t.Fatal(d)
			}
			resp := resource.UpdateResponse{State: state}
			r.Update(ctx, resource.UpdateRequest{State: state, Plan: plan}, &resp)
			if resp.Diagnostics.HasError() != tc.wantError {
				t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
			}
			if tc.wantError && !strings.Contains(resp.Diagnostics.Errors()[0].Summary(), "Foreign staged routing changes") {
				t.Fatalf("expected foreign staging refusal: %v", resp.Diagnostics)
			}
			if got := strings.Join(api.calls, ","); got != tc.wantCalls {
				t.Fatalf("calls %q, want %q", got, tc.wantCalls)
			}
		})
	}
}
