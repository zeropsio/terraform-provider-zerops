package project_vpn_peer

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	frameworkresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/zeropsio/terraform-provider-zerops/internal/acctest"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/provider"
	"os"
	"testing"
	"time"
)

func throwawayKey(t *testing.T) string {
	t.Helper()
	key, e := ecdh.X25519().GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	return base64.StdEncoding.EncodeToString(key.PublicKey().Bytes())
}
func vpnConfig(id, key1, key2 string, both bool) string {
	cfg := acctest.ProviderConfig() + fmt.Sprintf(`
resource "zerops_project_vpn_peer" "first" {
 project_id = %q
 public_key = %q
}
`, id, key1)
	if both {
		cfg += fmt.Sprintf(`
resource "zerops_project_vpn_peer" "second" {
 project_id = %q
 public_key = %q
}
`, id, key2)
	}
	return cfg
}
func TestAccVPNPeer(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC is not set")
	}
	acctest.PreCheck(t)
	p := acctest.CreateTestProject(t)
	c := acctest.Client(t)
	key1, key2, unknown := throwawayKey(t), throwawayKey(t), throwawayKey(t)
	cfg := vpnConfig(p.ID, key1, key2, true)
	var first client.VPNPeer
	checkRemoved := func(_ *terraform.State) error {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if _, err := c.GetProject(ctx, p.ID); err != nil {
			return err
		}
		for _, key := range []string{key1, key2, unknown} {
			if _, err := c.GetVPNPeer(ctx, p.ID, key); !client.IsNotFound(err, "vpnPublicKeyNotFound") {
				return fmt.Errorf("peer still registered while parent exists or unexpected read error: %v", err)
			}
		}
		return nil
	}
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(provider.Registry{Resources: []func() frameworkresource.Resource{NewResource}}), CheckDestroy: func(_ *terraform.State) error {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if _, err := c.GetProject(ctx, p.ID); client.IsNotFound(err, "projectNotFound") {
			return nil
		} else if err != nil {
			return err
		}
		for _, key := range []string{key1, key2, unknown} {
			_, e := c.GetVPNPeer(ctx, p.ID, key)
			if !client.IsNotFound(e, "vpnPublicKeyNotFound") {
				return fmt.Errorf("peer still registered or unexpected read error: %v", e)
			}
		}
		return nil
	}, Steps: []resource.TestStep{
		{Config: cfg, Check: resource.ComposeTestCheckFunc(resource.TestCheckResourceAttrSet("zerops_project_vpn_peer.first", "setup"), resource.TestCheckResourceAttrSet("zerops_project_vpn_peer.first", "setup_linux"), resource.TestCheckResourceAttrSet("zerops_project_vpn_peer.first", "setup_macos"), resource.TestCheckResourceAttrSet("zerops_project_vpn_peer.first", "setup_windows"), func(_ *terraform.State) error {
			var e error
			first, e = c.GetVPNPeer(context.Background(), p.ID, key1)
			if e != nil {
				return e
			}
			second, e := c.GetVPNPeer(context.Background(), p.ID, key2)
			if e != nil {
				return e
			}
			if first.IPv4.AssignedIPAddress == second.IPv4.AssignedIPAddress || first.IPv6.AssignedIPAddress == second.IPv6.AssignedIPAddress {
				return fmt.Errorf("peers received duplicate addresses")
			}
			return nil
		})},
		{Config: cfg, PlanOnly: true},
		{PreConfig: func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			repeat, e := c.CreateVPNPeer(ctx, p.ID, key1)
			if e != nil {
				t.Fatal(e)
			}
			if repeat.IPv4 != first.IPv4 || repeat.IPv6 != first.IPv6 {
				t.Fatal("repeated registration changed addresses")
			}
			if e = c.DeleteVPNPeer(ctx, p.ID, unknown); e != nil {
				t.Fatal(e)
			}
		}, Config: cfg, PlanOnly: true},
		{ResourceName: "zerops_project_vpn_peer.first", ImportState: true, ImportStateVerify: true},
		{Config: vpnConfig(p.ID, key1, key2, false), Check: func(_ *terraform.State) error {
			if _, e := c.GetVPNPeer(context.Background(), p.ID, key1); e != nil {
				return e
			}
			if _, e := c.GetVPNPeer(context.Background(), p.ID, key2); !client.IsNotFound(e, "vpnPublicKeyNotFound") {
				return fmt.Errorf("second peer not deleted: %v", e)
			}
			return nil
		}},
		{Config: vpnConfig(p.ID, key1, key2, false), PlanOnly: true},
		{Config: vpnConfig(p.ID, unknown, key2, false), Check: func(_ *terraform.State) error {
			if _, err := c.GetVPNPeer(context.Background(), p.ID, key1); !client.IsNotFound(err, "vpnPublicKeyNotFound") {
				return fmt.Errorf("replaced peer remains: %v", err)
			}
			_, err := c.GetVPNPeer(context.Background(), p.ID, unknown)
			return err
		}},
		{Config: vpnConfig(p.ID, unknown, key2, false), PlanOnly: true},
		{Config: acctest.ProviderConfig(), Check: checkRemoved},
		{Config: vpnConfig(p.ID, unknown, key2, false)},
		{PreConfig: func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
			defer cancel()
			if err := acctest.DeleteTestProject(ctx, c, p.ID); err != nil {
				t.Fatal(err)
			}
		}, RefreshState: true, ExpectNonEmptyPlan: true, Check: func(s *terraform.State) error {
			if _, ok := s.RootModule().Resources["zerops_project_vpn_peer.first"]; ok {
				return fmt.Errorf("absent project remains")
			}
			return nil
		}},
	}})
}
