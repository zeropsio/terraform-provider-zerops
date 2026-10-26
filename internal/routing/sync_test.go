package routing

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
)

type inventoryAPI struct {
	client.API
	http     []client.HTTPRoute
	services []client.Service
	ports    map[string][]client.PortRoute
	calls    []string
}

func (f *inventoryAPI) ListHTTPRoutes(_ context.Context, _ string, q client.PageQuery) (client.Page[client.HTTPRoute], error) {
	return client.Page[client.HTTPRoute]{Items: f.http, Total: len(f.http)}, nil
}
func (f *inventoryAPI) ListServices(_ context.Context, _ string, q client.ServiceQuery) (client.Page[client.Service], error) {
	return client.Page[client.Service]{Items: f.services, Total: len(f.services)}, nil
}
func (f *inventoryAPI) ListPortRoutes(_ context.Context, id string, q client.PageQuery) (client.Page[client.PortRoute], error) {
	f.calls = append(f.calls, id)
	return client.Page[client.PortRoute]{Items: f.ports[id], Total: len(f.ports[id])}, nil
}
func TestEffectiveForce(t *testing.T) {
	for _, tt := range []struct {
		v               types.Bool
		inherited, want bool
	}{{types.BoolNull(), true, true}, {types.BoolNull(), false, false}, {types.BoolValue(false), true, false}, {types.BoolValue(true), false, true}} {
		if got := EffectiveForce(tt.v, tt.inherited); got != tt.want {
			t.Fatalf("got %t want %t", got, tt.want)
		}
	}
}
func TestGateInventory(t *testing.T) {
	f := &inventoryAPI{http: []client.HTTPRoute{{ID: "own", DeleteOnSync: true}, {ID: "synced", IsSynced: true}, {ID: "foreign", Domains: []client.HTTPDomain{{DomainName: "a.example.com"}}, Locations: []client.HTTPLocation{{ServiceStackID: "backend", Port: 3000, Path: "/"}}}, {ID: "deleting", IsSynced: true, DeleteOnSync: true}}}
	items, err := HTTPInventory(context.Background(), f, "project", "own")
	if err != nil || len(items) != 2 {
		t.Fatalf("items=%v err=%v", items, err)
	}
	text := strings.Join(items, "\n")
	for _, word := range []string{"foreign", "a.example.com", "backend:3000/", "create/update", "deleting", "delete"} {
		if !strings.Contains(text, word) {
			t.Errorf("missing %q in %s", word, text)
		}
	}
	var refused diag.Diagnostics
	Gate(items, false, &refused)
	if !refused.HasError() {
		t.Fatal("foreign items must refuse")
	}
	var forced diag.Diagnostics
	Gate(items, true, &forced)
	if forced.HasError() || len(forced) != 1 || forced[0].Severity() != diag.SeverityWarning {
		t.Fatalf("expected warning: %v", forced)
	}
}
func TestPortInventoryAcrossServices(t *testing.T) {
	f := &inventoryAPI{services: []client.Service{{ID: "a"}, {ID: "b"}}, ports: map[string][]client.PortRoute{"a": {{ID: "own"}}, "b": {{ID: "foreign", ServiceStackID: "b", PortRouteWrite: client.PortRouteWrite{PublicPort: 20000, InternalPort: 8080, InternalProtocol: "udp"}}}}}
	items, e := PortInventory(context.Background(), f, "project", "own")
	if e != nil || len(items) != 1 || len(f.calls) != 2 {
		t.Fatalf("items %v calls %v error %v", items, f.calls, e)
	}
	for _, word := range []string{"foreign", "20000", "8080", "udp", "target_service=b"} {
		if !strings.Contains(items[0], word) {
			t.Errorf("missing %s", word)
		}
	}
}
