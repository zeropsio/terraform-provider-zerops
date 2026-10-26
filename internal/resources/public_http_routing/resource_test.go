package public_http_routing

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"testing"
)

func TestMappingAndVerification(t *testing.T) {
	ctx := context.Background()
	var m Model
	v := client.HTTPRoute{ID: "route", ProjectID: "project", IsEditable: true, DeleteOnSync: true, SSLEnabled: true, Domains: []client.HTTPDomain{{DomainName: "z.example.com", SSLStatus: "WAITING_FOR_DNS"}, {DomainName: "*.a.example.com", SSLStatus: "WAITING_FOR_DNS"}}, Locations: []client.HTTPLocation{{Path: "/", Port: 3000, ServiceStackID: "service"}}}
	if e := m.read(ctx, v); e != nil {
		t.Fatal(e)
	}
	if m.IsSynced.ValueBool() {
		t.Fatal("pending deletion must read unsynced")
	}
	v.DeleteOnSync = false
	v.IsSynced = true
	if e := m.read(ctx, v); e != nil {
		t.Fatal(e)
	}
	desired, e := m.write(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = m.verify(ctx, v, desired); e != nil {
		t.Fatal(e)
	}
	v.IsSynced = false
	if e = m.verify(ctx, v, desired); e == nil {
		t.Fatal("nothingToSync cannot accept unsynced route")
	}
	v.IsEditable = false
	if e = m.read(ctx, v); e == nil {
		t.Fatal("generated routes must not be imported")
	}
	if m.DomainStatuses.IsNull() {
		t.Fatal("statuses missing")
	}
	_ = types.BoolNull()
}
