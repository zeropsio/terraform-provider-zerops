package client

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestVPNPeerWire(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte{251, 255, 255, 251, 255, 255, 251, 255, 255, 251, 255, 255, 251, 255, 255, 251, 255, 255, 251, 255, 255, 251, 255, 255, 251, 255, 255, 251, 255, 255, 251, 255})
	if !strings.ContainsAny(key, "+/") || !strings.HasSuffix(key, "=") {
		t.Fatal("fixture must exercise standard base64")
	}
	calls := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method == "GET" {
			if r.URL.Path != "/api/rest/public/project/p/vpn/"+base64.URLEncoding.EncodeToString([]byte(key)) {
				t.Errorf("key not text-encoded with padding: %s", r.URL.Path)
			}
		} else {
			var b struct {
				PublicKey string `json:"publicKey"`
			}
			if e := json.NewDecoder(r.Body).Decode(&b); e != nil || b.PublicKey != key {
				t.Error("key body missing")
			}
			if r.URL.Path != "/api/rest/public/project/p/vpn" {
				t.Error("bad mutation path")
			}
		}
		if r.Method == "DELETE" {
			respond(w, 200, `{"success":true}`)
			return
		}
		body, _ := json.Marshal(map[string]any{"peer": VPNPeer{PublicKey: key, IPv4: VPNPeerAddress{AssignedIPAddress: "10.0.0.3"}, IPv6: VPNPeerAddress{AssignedIPAddress: "fd00::3"}, Setup: "public", SetupLinux: "linux", SetupMacOS: "mac", SetupWindows: "windows"}})
		respond(w, 200, string(body))
	})
	p, e := c.CreateVPNPeer(context.Background(), "p", key)
	if e != nil || p.SetupMacOS != "mac" {
		t.Fatal(p, e)
	}
	p, e = c.GetVPNPeer(context.Background(), "p", key)
	if e != nil || p.IPv4.AssignedIPAddress != "10.0.0.3" {
		t.Fatal(p, e)
	}
	if e = c.DeleteVPNPeer(context.Background(), "p", key); e != nil {
		t.Fatal(e)
	}
	if calls != 3 {
		t.Fatal(calls)
	}
}
