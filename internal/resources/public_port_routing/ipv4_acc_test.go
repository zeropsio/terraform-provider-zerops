package public_port_routing

import (
	"context"
	"fmt"
	frameworkresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/zeropsio/terraform-provider-zerops/internal/acctest"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/provider"
	"github.com/zeropsio/terraform-provider-zerops/internal/routing"
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestAccIPv4Prerequisite(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC required")
	}
	if acctest.IsProd() {
		t.Skip("production IPv4 tests use only an existing holder fixture")
	}
	acctest.PreCheck(t)
	p := acctest.CreateTestProject(t)
	s := acctest.DeployHTTPApp(t, p.ID)
	config := acctest.ProviderConfig() + fmt.Sprintf(`resource "zerops_public_port_routing" "test" {
 service_id=%q
 public_port=23084
 internal_port=3000
 public_ip_type="IP_V4"
 }`, s.ID)
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(provider.Registry{Resources: []func() frameworkresource.Resource{NewResource}}), Steps: []resource.TestStep{
		{Config: config, ExpectError: regexp.MustCompile("(?s)publicIpTypeNotSupported.*dedicated project IPv4")},
		{Config: acctest.ProviderConfig(), Check: func(st *terraform.State) error {
			if len(st.RootModule().Resources) != 0 {
				return fmt.Errorf("failed prerequisite left resource state")
			}
			routes, err := acctest.Client(t).ListPortRoutes(context.Background(), s.ID, client.PageQuery{Limit: 100})
			if err != nil {
				return err
			}
			if len(routes.Items) != 0 || routes.Total != 0 {
				return fmt.Errorf("failed prerequisite created routing")
			}
			return nil
		}},
	}})
}
func TestAccIPv4HolderRouting(t *testing.T) {
	p := acctest.DedicatedIPv4Fixture(t)
	api := acctest.Client(t)
	// Never sync the holder if it already has foreign staged changes.
	staged, err := routing.PortInventory(context.Background(), api, p.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(staged) != 0 {
		t.Skip("holder has staged routing; refusing to synchronize unrelated changes")
	}
	s := acctest.DeployHTTPAppChild(t, p.ID)
	config := func(update bool) string {
		port, policy, ranges := 23084, "BLACKLIST", "[]"
		if update {
			port, policy, ranges = 23085, "WHITELIST", `["192.0.2.1/32"]`
		}
		return acctest.ProviderConfig() + fmt.Sprintf(`resource "zerops_public_port_routing" "test" {
 service_id=%q
 public_port=%d
 internal_port=3000
 public_ip_type="IP_V4"
 firewall_policy=%q
 firewall_ip_ranges=%s
 }`, s.ID, port, policy, ranges)
	}
	// Bare hosts retain configured spelling across refresh. Import has no prior
	// spelling, so verify every firewall field against canonical CIDR config.
	bareConfig := strings.Replace(config(true), "192.0.2.1/32", "192.0.2.1", 1)
	var id string
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: acctest.AdoptOnlyIPv4Factories(provider.Registry{Resources: []func() frameworkresource.Resource{NewResource}}), CheckDestroy: func(_ *terraform.State) error {
		if id != "" {
			_, err := api.GetPortRoute(context.Background(), id)
			if !client.IsNotFound(err, "publicPortRoutingNotFound") {
				return fmt.Errorf("IPv4 route not absent: %v", err)
			}
		}
		got, err := api.GetProject(context.Background(), p.ID)
		if err != nil {
			return err
		}
		if got.PublicIPv4 == nil || *got.PublicIPv4 != *p.PublicIPv4 {
			return fmt.Errorf("holder allocation changed")
		}
		return nil
	}, Steps: []resource.TestStep{
		{Config: config(false), Check: resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr("zerops_public_port_routing.test", "public_ip_type", "IP_V4"), func(st *terraform.State) error {
			id = st.RootModule().Resources["zerops_public_port_routing.test"].Primary.ID
			return nil
		})},
		{Config: bareConfig, Check: resource.TestCheckResourceAttr("zerops_public_port_routing.test", "firewall_ip_ranges.0", "192.0.2.1")},
		{Config: bareConfig, PlanOnly: true},
		{Config: config(true), Check: resource.TestCheckResourceAttr("zerops_public_port_routing.test", "public_port", "23085")},
		{ResourceName: "zerops_public_port_routing.test", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"timeouts"}},
		{Config: config(true), PlanOnly: true},
		{PreConfig: func() {
			v, err := api.GetPortRoute(context.Background(), id)
			if err != nil {
				t.Fatal(err)
			}
			v.PublicPort = 23086
			if _, err = api.UpdatePortRoute(context.Background(), id, v.PortRouteWrite); err != nil {
				t.Fatal(err)
			}
			if err = routing.Sync(context.Background(), api, p.ID, false); err != nil {
				t.Fatal(err)
			}
		}, Config: config(true)},
		{Config: config(true), PlanOnly: true},
		{Config: acctest.ProviderConfig()},
	}})
}
