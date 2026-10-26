package project_vpn_peer

import (
	"context"
	"encoding/base64"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
	"strings"
	"testing"
)

func TestVPNCanonicalKeysAndImports(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("\xff", 32)))
	if !validKey(key) || !strings.Contains(key, "/") {
		t.Fatal("bad fixture")
	}
	for _, k := range []string{"", key + "=", strings.TrimSuffix(key, "="), strings.ReplaceAll(key, "/", "_")} {
		if validKey(k) {
			t.Fatal("invalid canonical key accepted")
		}
	}
	r := &configResource{}
	for _, id := range []string{"", "p", "/" + key, "p/invalid", "p/" + key + "/"} {
		resp := resource.ImportStateResponse{}
		r.ImportState(context.Background(), resource.ImportStateRequest{ID: id}, &resp)
		if !resp.Diagnostics.HasError() {
			t.Fatalf("malformed import accepted: %q", id)
		}
	}
	f := &fakeAPI{peer: client.VPNPeer{PublicKey: key, IPv4: client.VPNPeerAddress{AssignedIPAddress: "10.0.0.3"}, IPv6: client.VPNPeerAddress{AssignedIPAddress: "fd00::3"}}}
	r.data = &providerdata.ProviderData{API: f}
	var s resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &s)
	resp := resource.ImportStateResponse{State: tfsdk.State{Schema: s.Schema}}
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: "p/" + key}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	var imported model
	if d := resp.State.Get(context.Background(), &imported); d.HasError() || imported.PublicKey.ValueString() != key || imported.ID.ValueString() != "p/"+key {
		t.Fatal("slash-containing key import did not round-trip", d)
	}
}

type fakeAPI struct {
	client.API
	peer client.VPNPeer
	err  error
}

func (f *fakeAPI) GetVPNPeer(context.Context, string, string) (client.VPNPeer, error) {
	return f.peer, f.err
}
func TestVPNReadMapsAddressDriftAndSetup(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	f := &fakeAPI{peer: client.VPNPeer{PublicKey: key, IPv4: client.VPNPeerAddress{AssignedIPAddress: "10.0.0.3"}, IPv6: client.VPNPeerAddress{AssignedIPAddress: "fd00::3"}, Setup: "public", SetupLinux: "linux", SetupMacOS: "mac", SetupWindows: "windows"}}
	r := &configResource{data: &providerdata.ProviderData{API: f}}
	m := model{ProjectID: types.StringValue("p"), PublicKey: types.StringValue(key)}
	ok, e := r.load(context.Background(), &m)
	if !ok || e != nil || m.ID.ValueString() != "p/"+key || m.SetupMacOS.ValueString() != "mac" {
		t.Fatal("peer mapping failed", e)
	}
	f.peer.IPv4.AssignedIPAddress = "10.0.0.4"
	if _, e = r.load(context.Background(), &m); e != nil || m.IPv4Address.ValueString() != "10.0.0.4" {
		t.Fatal("drift masked")
	}
	f.peer.IPv6.AssignedIPAddress = ""
	if _, e = r.load(context.Background(), &m); e == nil {
		t.Fatal("malformed addresses accepted")
	}
	f.err = &client.APIError{HTTPStatus: 403, Code: "vpnPublicKeyNotFound"}
	if _, e = r.load(context.Background(), &m); e == nil {
		t.Fatal("auth interpreted as absence")
	}
	f.err = &client.APIError{HTTPStatus: 400, Code: "vpnPublicKeyNotFound"}
	if ok, e = r.load(context.Background(), &m); ok || e != nil {
		t.Fatal("absence not removed")
	}
}
