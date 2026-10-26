package public_http_routing

import (
	"fmt"
	frameworkresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/zeropsio/terraform-provider-zerops/internal/acctest"
	"github.com/zeropsio/terraform-provider-zerops/internal/provider"
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestAccLocationConfig(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC required")
	}
	acctest.PreCheck(t)
	p := acctest.CreateTestProject(t)
	s := acctest.DeployHTTPApp(t, p.ID)
	domain := acctest.RandomName("tfacc") + ".example.com"
	config := func(enabled bool, edited bool, remove bool) string {
		target := "/target"
		if edited {
			target = "/edited"
		}
		blocks := []string{
			fmt.Sprintf(`redirect={enabled=%t,to=%q,code=302,preserve_path=true,preserve_query=true}`, enabled, target),
			fmt.Sprintf(`basic_auth={enabled=%t,realm="TestRealm",users=[{username="testuser",password="fixture-only-not-a-real-secret",comment="fixture"}]}`, enabled),
			fmt.Sprintf(`access_policy={enabled=%t,cidr=["192.0.2.0/24","2001:db8::/32"],default_policy="deny",real_ip_header="X-Forwarded-For",set_real_ip_from=["127.0.0.0/8"]}`, enabled),
			fmt.Sprintf(`content={enabled=%t,code=200,content="fixture",content_type="text/plain"}`, enabled),
			fmt.Sprintf(`rate_limiting={enabled=%t,key="server_name",zone="testzone",rate=12,burst=15,zone_size=10}`, enabled),
		}
		locations := make([]string, len(blocks))
		for i, b := range blocks {
			extra := ",config={" + b + "}"
			if remove {
				extra = ""
			}
			locations[i] = fmt.Sprintf(`{path="/test%d",port=3000,service_id=%q%s}`, i, s.ID, extra)
		}
		return acctest.ProviderConfig() + fmt.Sprintf(`resource "zerops_public_http_routing" "test" {
project_id=%q
 domains=[%q]
 ssl_enabled=false
 location=[%s]
}`, p.ID, domain, strings.Join(locations, ","))
	}
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(provider.Registry{Resources: []func() frameworkresource.Resource{NewResource}}), Steps: []resource.TestStep{
		{Config: config(true, false, false), Check: resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr("zerops_public_http_routing.test", "location.0.config.redirect.to", "/target"), resource.TestCheckResourceAttr("zerops_public_http_routing.test", "location.1.config.basic_auth.users.0.password", "fixture-only-not-a-real-secret"))},
		{ResourceName: "zerops_public_http_routing.test", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"timeouts", "domain_statuses"}},
		{Config: config(true, true, false), Check: resource.TestCheckResourceAttr("zerops_public_http_routing.test", "location.0.config.redirect.to", "/edited")},
		{Config: config(false, true, false), Check: resource.TestCheckResourceAttr("zerops_public_http_routing.test", "location.0.config.redirect.enabled", "false")},
		{Config: config(false, true, true)},
	}})
}

func TestAccSSLReadiness(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC required")
	}
	if os.Getenv("ZEROPS_ACC_DNS") != "1" {
		t.Skip("ZEROPS_ACC_DNS=1 and restricted Cloudflare helper credentials required")
	}
	acctest.PreCheck(t)
	p := acctest.CreateTestProject(t)
	s := acctest.DeployHTTPApp(t, p.ID)
	dnsDomain := os.Getenv("ZEROPS_ACC_DNS_DOMAIN")
	if dnsDomain == "" {
		t.Fatal("ZEROPS_ACC_DNS_DOMAIN is required for DNS acceptance")
	}
	domain := acctest.RandomName("tfacc") + "." + dnsDomain
	if p.PublicIPv6 == nil || *p.PublicIPv6 == "" {
		t.Fatal("project has no public IPv6 for the DNS challenge")
	}
	acctest.CreateDNSRecord(t, "AAAA", domain, *p.PublicIPv6)
	config := acctest.ProviderConfig() + fmt.Sprintf(`resource "zerops_public_http_routing" "test" {
project_id=%q
 domains=[%q]
 wait_for_ssl=true
 ssl_timeout="10m"
 location=[{path="/",port=3000,service_id=%q}]
}`, p.ID, domain, s.ID)
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(provider.Registry{Resources: []func() frameworkresource.Resource{NewResource}}), Steps: []resource.TestStep{
		{Config: config, Check: resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr("zerops_public_http_routing.test", "domain_statuses.0.domain", domain), resource.TestCheckResourceAttr("zerops_public_http_routing.test", "domain_statuses.0.ssl_status", "ACTIVE"))},
		{ResourceName: "zerops_public_http_routing.test", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"timeouts", "domain_statuses", "wait_for_ssl", "ssl_timeout"}},
	}})
}

func TestAccSSLNoDNS(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC required")
	}
	if os.Getenv("ZEROPS_ACC_DNS") != "1" {
		t.Skip("ZEROPS_ACC_DNS=1 required for SSL acceptance")
	}
	acctest.PreCheck(t)
	p := acctest.CreateTestProject(t)
	s := acctest.DeployHTTPApp(t, p.ID)
	domain := acctest.RandomName("tfacc") + ".example.com"
	config := acctest.ProviderConfig() + fmt.Sprintf(`resource "zerops_public_http_routing" "test" {
	project_id=%q
	domains=[%q]
	wait_for_ssl=true
	ssl_timeout="12s"
	location=[{path="/",port=3000,service_id=%q}]
}`, p.ID, domain, s.ID)
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(provider.Registry{Resources: []func() frameworkresource.Resource{NewResource}}), Steps: []resource.TestStep{
		{Config: config, ExpectError: regexp.MustCompile(`(?s)SSL readiness.*context\s+deadline\s+exceeded`)},
		{Config: strings.Replace(config, "wait_for_ssl=true", "wait_for_ssl=false", 1), Check: resource.TestCheckResourceAttr("zerops_public_http_routing.test", "is_synced", "true")},
	}})
}
