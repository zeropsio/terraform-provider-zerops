package public_http_routing

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
	"github.com/zeropsio/terraform-provider-zerops/internal/waiter"
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
	api := acctest.Client(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()
	domain := acctest.RandomName("tfacc") + ".example.com"
	var own, foreign string
	config := func(update bool, force string, providerForce bool) string {
		h := acctest.ProviderConfig()
		if providerForce {
			h = strings.Replace(h, `provider "zerops" {`, "provider \"zerops\" {\n  routing_force_sync = true", 1)
		}
		extra := ""
		if force != "" {
			extra = "force_sync = " + force
		}
		ssl := "true"
		path := "/"
		name := domain
		cdn := "false"
		if update {
			path = "/api"
			name = "updated." + domain
			cdn = "true"
		}
		return h + fmt.Sprintf(`resource "zerops_public_http_routing" "test" {
project_id=%q
 domains=[%q,%q]
ssl_enabled=%s
cdn_enabled=%s
location=[{path=%q,port=3000,service_id=%q}]
%s
}`, p.ID, name, "*."+name, ssl, cdn, path, s.ID, extra)
	}
	capture := func(st *terraform.State) error {
		id := st.RootModule().Resources["zerops_public_http_routing.test"].Primary.ID
		if own != "" && own != id {
			return fmt.Errorf("in-place update changed routing ID from %s to %s", own, id)
		}
		own = id
		return nil
	}
	stageOwn := func(deleting bool) func() {
		return func() {
			if deleting {
				if err := api.DeleteHTTPRoute(ctx, own); err != nil {
					t.Fatal(err)
				}
			} else {
				v, e := api.GetHTTPRoute(ctx, own)
				if e != nil {
					t.Fatal(e)
				}
				domains := make([]string, len(v.Domains))
				for i, d := range v.Domains {
					domains[i] = d.DomainName
				}
				_, e = api.UpdateHTTPRoute(ctx, own, client.HTTPRouteWrite{Domains: domains, SSLEnabled: v.SSLEnabled, CDNEnabled: &v.CDNEnabled, Locations: v.Locations})
				if e != nil {
					t.Fatal(e)
				}
			}
		}
	}
	stageForeign := func() {
		cdn := false
		v, e := api.CreateHTTPRoute(ctx, p.ID, client.HTTPRouteWrite{Domains: []string{acctest.RandomName("tfacc") + ".example.com"}, SSLEnabled: false, CDNEnabled: &cdn, Locations: []client.HTTPLocation{{Path: "/", Port: 3000, ServiceStackID: s.ID}}})
		if e != nil {
			t.Fatal(e)
		}
		foreign = v.ID
		// A policy-only force_sync change does not synchronize an already-applied
		// owned route. Require reconciliation so the foreign-stage gate is exercised.
		if own != "" {
			stageOwn(false)()
		}
	}
	stageForeignUpdate := func() {
		v, e := api.GetHTTPRoute(ctx, foreign)
		if e != nil {
			t.Fatal(e)
		}
		domains := make([]string, len(v.Domains))
		for i, d := range v.Domains {
			domains[i] = d.DomainName
		}
		_, e = api.UpdateHTTPRoute(ctx, foreign, client.HTTPRouteWrite{Domains: domains, SSLEnabled: !v.SSLEnabled, CDNEnabled: &v.CDNEnabled, Locations: v.Locations})
		if e != nil {
			t.Fatal(e)
		}
		stageOwn(false)()
	}
	check := resource.ComposeTestCheckFunc(capture, resource.TestCheckResourceAttr("zerops_public_http_routing.test", "is_synced", "true"), resource.TestCheckResourceAttrSet("zerops_public_http_routing.test", "domain_statuses.0.ssl_status"))
	resource.Test(t, resource.TestCase{PreCheck: func() { acctest.PreCheck(t) }, ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(provider.Registry{Resources: []func() frameworkresource.Resource{NewResource}}), Steps: []resource.TestStep{
		{PreConfig: stageForeign, Config: config(false, "", false), ExpectError: regexp.MustCompile("(?s)Foreign staged routing changes.*id=.*pending_action")},
		{Config: config(false, "true", false), Check: check},
		{Config: config(false, "", false), Check: check},
		// DNS/SSL/CDN observations change asynchronously between state capture and
		// import (e.g. CHECKING -> FAILED without valid DNS); they are not inputs.
		{ResourceName: "zerops_public_http_routing.test", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"timeouts", "domain_statuses"}},
		{ResourceName: "zerops_public_http_routing.test", ImportState: true, ImportStateIdFunc: func(_ *terraform.State) (string, error) {
			process, e := api.SetSubdomainAccess(ctx, s.ID, true)
			if e != nil {
				return "", e
			}
			if _, e = waiter.WaitProcess(ctx, api, process.ID); e != nil {
				return "", e
			}
			page, e := api.ListHTTPRoutes(ctx, p.ID, client.PageQuery{Limit: 100})
			if e != nil {
				return "", e
			}
			for _, v := range page.Items {
				if !v.IsEditable {
					return v.ID, nil
				}
			}
			return "", fmt.Errorf("no generated route after enabling subdomain access")
		}, ExpectError: regexp.MustCompile("generated/read-only")},
		{Config: config(true, "", false), Check: check},
		{Config: strings.Replace(strings.Replace(config(true, "", false), "ssl_enabled=true", "ssl_enabled=false", 1), "cdn_enabled=true", "cdn_enabled=false", 1), Check: check},
		{Config: config(true, "", false), Check: check},
		{PreConfig: stageOwn(false), Config: config(true, "", false), PlanOnly: true, ExpectNonEmptyPlan: true, ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectResourceAction("zerops_public_http_routing.test", plancheck.ResourceActionUpdate)}}},
		{Config: config(true, "", false), Check: check},
		{PreConfig: stageOwn(true), Config: config(true, "", false), PlanOnly: true, ExpectNonEmptyPlan: true, ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectResourceAction("zerops_public_http_routing.test", plancheck.ResourceActionUpdate)}}},
		{Config: config(true, "", false), Check: check},
		{PreConfig: stageForeign, Config: config(true, "false", false), ExpectError: regexp.MustCompile("Foreign staged routing changes")},
		{Config: config(true, "true", false), Check: resource.ComposeTestCheckFunc(check, func(_ *terraform.State) error {
			v, e := api.GetHTTPRoute(ctx, foreign)
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
			if e := api.DeleteHTTPRoute(ctx, foreign); e != nil {
				t.Fatal(e)
			}
			stageOwn(false)()
		}, Config: config(true, "false", true), ExpectError: regexp.MustCompile("(?s)Foreign staged routing changes.*pending_action=delete")},
		{Config: config(true, "", true), Check: check},
		{Config: strings.Replace(config(true, "", true), p.ID, "replacement-project-id", 1), PlanOnly: true, ExpectNonEmptyPlan: true, ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectResourceAction("zerops_public_http_routing.test", plancheck.ResourceActionDestroyBeforeCreate)}}},
		{Config: acctest.ProviderConfig()},
		{PreConfig: func() { own = "" }, Config: config(true, "", true), Check: check},
		{PreConfig: func() {
			if e := api.DeleteHTTPRoute(ctx, own); e != nil {
				t.Fatal(e)
			}
			if e := routing.Sync(ctx, api, p.ID, true); e != nil {
				t.Fatal(e)
			}
		}, Config: config(true, "", true), PlanOnly: true, ExpectNonEmptyPlan: true},
	}})
}
