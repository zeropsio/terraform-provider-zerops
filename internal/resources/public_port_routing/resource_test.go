package public_port_routing

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"testing"
)

func TestMappingAndVerification(t *testing.T) {
	ctx := context.Background()
	var m Model
	v := client.PortRoute{ID: "route", ServiceStackID: "service", DeleteOnSync: true, PortRouteWrite: client.PortRouteWrite{PublicIPType: "IP_V6", PublicPort: 20000, InternalPort: 3000, InternalProtocol: "tcp", FirewallPolicy: "BLACKLIST", FirewallIPRanges: []string{}}}
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
	v.PublicIPType = "IP_V4"
	if e = m.read(ctx, v); e != nil {
		t.Fatalf("IPv4 imports must be supported: %v", e)
	}
	m.FirewallIPRanges = types.SetValueMust(types.StringType, nil)
	desired, e = m.write(ctx)
	if e != nil || desired.FirewallIPRanges == nil || desired.FirewallAllowMyIP {
		t.Fatal("must send explicit empty ranges and false allowMyIp")
	}
}
