package public_port_routing

import (
	"context"
	"fmt"
	frameworkresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/zeropsio/terraform-provider-zerops/internal/acctest"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/provider"
	"github.com/zeropsio/terraform-provider-zerops/internal/routing"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestAccRouting(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC required")
	}
	acctest.PreCheck(t)
	p := acctest.CreateTestProject(t)
	s := acctest.DeployHTTPApp(t, p.ID)
	other := acctest.DeployHTTPApp(t, p.ID)
	api := acctest.Client(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()

	var own, foreign string
	foreignCount := 0
	config := func(update bool, force string, providerForce bool) string {
		h := acctest.ProviderConfig()
		if providerForce {
			h = strings.Replace(h, `provider "zerops" {`, "provider \"zerops\" {\n  routing_force_sync = true", 1)
		}
		extra := ""
		if force != "" {
			extra = "force_sync = " + force
		}
		protocol := "tcp"
		internal := 3000
		public := 20000
		policy := "BLACKLIST"
		ranges := "[]"
		if update {
			protocol = "udp"
			internal = 8080
			public = 20001
			policy = "WHITELIST"
			ranges = `["2001:db8::/32","2001:db8::1"]`
		}
		return h + fmt.Sprintf(`resource "zerops_public_port_routing" "test" {
service_id=%q
public_port=%d
internal_port=%d
protocol=%q
firewall_policy=%q
firewall_ip_ranges=%s
%s
}`, s.ID, public, internal, protocol, policy, ranges, extra)
	}
	capture := func(st *terraform.State) error {
		id := st.RootModule().Resources["zerops_public_port_routing.test"].Primary.ID
		if own != "" && own != id {
			return fmt.Errorf("in-place update changed routing ID from %s to %s", own, id)
		}
		own = id
		return nil
	}
	stageOwn := func(deleting bool) func() {
		return func() {
			if deleting {
				if err := api.DeletePortRoute(ctx, own); err != nil {
					t.Fatal(err)
				}
			} else {
				v, e := api.GetPortRoute(ctx, own)
				if e != nil {
					t.Fatal(e)
				}
				_, e = api.UpdatePortRoute(ctx, own, v.PortRouteWrite)
				if e != nil {
					t.Fatal(e)
				}
			}
		}
	}
	stageForeign := func() {
		foreignCount++
		v, e := api.CreatePortRoute(ctx, other.ID, client.PortRouteWrite{PublicIPType: "IP_V6", PublicPort: 21000 + foreignCount, InternalPort: 3000, InternalProtocol: "tcp", FirewallPolicy: "BLACKLIST", FirewallIPRanges: []string{}})
		if e != nil {
			t.Fatal(e)
		}
		foreign = v.ID
	}
	stageForeignUpdate := func() {
		v, e := api.GetPortRoute(ctx, foreign)
		if e != nil {
			t.Fatal(e)
		}
		v.PublicPort++
		_, e = api.UpdatePortRoute(ctx, foreign, v.PortRouteWrite)
		if e != nil {
			t.Fatal(e)
		}
	}
	check := resource.ComposeTestCheckFunc(capture, resource.TestCheckResourceAttr("zerops_public_port_routing.test", "is_synced", "true"), resource.TestCheckResourceAttr("zerops_public_port_routing.test", "public_ip_type", "IP_V6"))
	resource.Test(t, resource.TestCase{PreCheck: func() { acctest.PreCheck(t) }, ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(provider.Registry{Resources: []func() frameworkresource.Resource{NewResource}}), Steps: []resource.TestStep{
		{PreConfig: stageForeign, Config: config(false, "", false), ExpectError: regexp.MustCompile("(?s)Foreign staged routing changes.*id=.*pending_action")},
		{Config: config(false, "true", false), Check: check},
		{Config: config(false, "", false), Check: check},
		{ResourceName: "zerops_public_port_routing.test", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"timeouts"}},
		{Config: strings.Replace(config(false, "", false), "public_port=20000", "public_port=80", 1), ExpectError: regexp.MustCompile("Invalid public port")},
		{Config: strings.Replace(config(false, "", false), "public_port=20000", "public_port=443", 1), ExpectError: regexp.MustCompile("Invalid public port")},
		{Config: strings.Replace(config(false, "", false), "internal_port=3000", "public_ip_type=\"IP_V4\"\ninternal_port=3000", 1), ExpectError: regexp.MustCompile("(?s)publicIpTypeNotSupported.*dedicated project IPv4")},
		{Config: config(true, "", false), Check: check},
		{PreConfig: stageOwn(false), Config: config(true, "", false), PlanOnly: true, ExpectNonEmptyPlan: true, ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectResourceAction("zerops_public_port_routing.test", plancheck.ResourceActionUpdate)}}},
		{Config: config(true, "", false), Check: check},
		{PreConfig: stageOwn(true), Config: config(true, "", false), PlanOnly: true, ExpectNonEmptyPlan: true, ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectResourceAction("zerops_public_port_routing.test", plancheck.ResourceActionUpdate)}}},
		{Config: config(true, "", false), Check: check},
		{PreConfig: stageForeign, Config: config(true, "false", false), ExpectError: regexp.MustCompile("Foreign staged routing changes")},
		{Config: config(true, "true", false), Check: resource.ComposeTestCheckFunc(check, func(_ *terraform.State) error {
			v, e := api.GetPortRoute(ctx, foreign)
			if e != nil {
				return e
			}
			if !v.IsSynced {
				return fmt.Errorf("foreign route not synced")
			}
			return nil
		})},
		{PreConfig: stageForeign, Config: config(true, "false", true), ExpectError: regexp.MustCompile("Foreign staged routing changes")},
		{Config: config(true, "", true), Check: check},
		{PreConfig: stageForeignUpdate, Config: config(true, "false", true), ExpectError: regexp.MustCompile("(?s)Foreign staged routing changes.*id=.*pending_action")},
		{Config: config(true, "true", true), Check: check},
		{PreConfig: func() {
			if e := api.DeletePortRoute(ctx, foreign); e != nil {
				t.Fatal(e)
			}
		}, Config: config(true, "false", true), ExpectError: regexp.MustCompile("(?s)Foreign staged routing changes.*pending_action=delete")},
		{Config: config(true, "", true), Check: check},
		{Config: strings.Replace(config(true, "", true), s.ID, "replacement-service-id", 1), PlanOnly: true, ExpectNonEmptyPlan: true, ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectResourceAction("zerops_public_port_routing.test", plancheck.ResourceActionDestroyBeforeCreate)}}},
		{Config: acctest.ProviderConfig()},
		{PreConfig: func() { own = "" }, Config: config(true, "", true), Check: check},
		{PreConfig: func() {
			if e := api.DeletePortRoute(ctx, own); e != nil {
				t.Fatal(e)
			}
			if e := routing.Sync(ctx, api, p.ID, false); e != nil {
				t.Fatal(e)
			}
		}, Config: config(true, "", true), PlanOnly: true, ExpectNonEmptyPlan: true},
	}})
}
