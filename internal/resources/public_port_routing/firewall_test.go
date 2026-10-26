package public_port_routing

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"testing"
)

func TestFirewallAddressRoundTrip(t *testing.T) {
	ctx := context.Background()
	m := Model{FirewallIPRanges: types.SetValueMust(types.StringType, []attr.Value{types.StringValue("192.0.2.1"), types.StringValue("2001:db8::1"), types.StringValue("2001:db8::/32")})}
	desired, e := m.write(ctx)
	if e != nil {
		t.Fatal(e)
	}
	seen := map[string]bool{}
	for _, value := range desired.FirewallIPRanges {
		seen[value] = true
	}
	for _, want := range []string{"192.0.2.1/32", "2001:db8::1/128", "2001:db8::/32"} {
		if !seen[want] {
			t.Errorf("missing canonical range %s in %v", want, desired.FirewallIPRanges)
		}
	}
	desired.PublicIPType = "IP_V6"
	v := client.PortRoute{ID: "route", PortRouteWrite: desired, IsSynced: true}
	prior := m.FirewallIPRanges
	if e = m.read(ctx, v); e != nil {
		t.Fatal(e)
	}
	if !m.FirewallIPRanges.Equal(prior) {
		t.Fatalf("read changed configured address spelling: %s", m.FirewallIPRanges)
	}
	// Imports have no prior spelling and expose the actual CIDRs.
	imported := Model{}
	v.FirewallIPRanges = []string{"192.0.2.1/32"}
	if e = imported.read(ctx, v); e != nil {
		t.Fatal(e)
	}
	var ranges []string
	d := imported.FirewallIPRanges.ElementsAs(ctx, &ranges, false)
	if d.HasError() || len(ranges) != 1 || ranges[0] != "192.0.2.1/32" {
		t.Fatalf("import %v %v", ranges, d)
	}
}

func TestFirewallSemanticEquivalence(t *testing.T) {
	ctx := context.Background()
	for _, tt := range []struct {
		name, configured, canonical, drift string
	}{
		{"ipv4", "192.0.2.1", "192.0.2.1/32", "192.0.2.2/32"},
		{"ipv6", "2001:0db8::1", "2001:db8::1/128", "2001:db8::2/128"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			prior := types.SetValueMust(types.StringType, []attr.Value{types.StringValue(tt.configured)})
			m := Model{PublicIPType: types.StringValue("IP_V4"), FirewallIPRanges: prior}
			desired, err := m.write(ctx)
			if err != nil {
				t.Fatal(err)
			}
			v := client.PortRoute{ID: "route", IsSynced: true, PortRouteWrite: client.PortRouteWrite{PublicIPType: "IP_V4", FirewallIPRanges: []string{tt.canonical}}}
			if err := m.verify(ctx, v, desired); err != nil {
				t.Fatalf("equivalent host CIDR rejected: %v", err)
			}
			for range 2 {
				v.FirewallIPRanges = []string{tt.canonical}
				if err := m.read(ctx, v); err != nil {
					t.Fatal(err)
				}
				if !m.FirewallIPRanges.Equal(prior) {
					t.Fatal("refresh changed configured host spelling")
				}
			}
			imported := Model{}
			v.FirewallIPRanges = []string{tt.canonical}
			if err := imported.read(ctx, v); err != nil {
				t.Fatal(err)
			}
			canonical := types.SetValueMust(types.StringType, []attr.Value{types.StringValue(tt.canonical)})
			if !imported.FirewallIPRanges.Equal(canonical) {
				t.Fatal("import must expose canonical CIDR without prior spelling")
			}
			v.FirewallIPRanges = []string{tt.drift}
			if err := m.verify(ctx, v, desired); err == nil {
				t.Fatal("different host was accepted as equivalent")
			}
			if err := m.read(ctx, v); err != nil {
				t.Fatal(err)
			}
			if m.FirewallIPRanges.Equal(prior) {
				t.Fatal("refresh hid firewall drift")
			}
		})
	}
}
