package project_prometheus_config

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"testing"
	"time"

	frameworkresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/zeropsio/terraform-provider-zerops/internal/acctest"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/provider"
	"github.com/zeropsio/terraform-provider-zerops/internal/waiter"
)

func TestTargets(t *testing.T) {
	for _, s := range []string{"localhost", "127.0.0.1:9100", "[::1]:65535", "::1", "metrics.example:1"} {
		if !validTarget(s) {
			t.Errorf("rejected %s", s)
		}
	}
	for _, s := range []string{"", "localhost:70000", "localhost:0", "https://localhost", "foo/bar", "[::1]:-1", "foo:", "-bad:80"} {
		if validTarget(s) {
			t.Errorf("accepted %s", s)
		}
	}
}
func config(id, target, label string) string {
	return acctest.ProviderConfig() + fmt.Sprintf(`
resource "zerops_project_prometheus_config" "test" {
 project_id = %q
 configs = [{ targets = [%q], labels = { job = %q, environment = "sandbox" } }]
}
`, id, target, label)
}
func TestAccPrometheusConfig(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC is not set")
	}
	acctest.PreCheck(t)
	project := acctest.CreateTestProject(t)
	c := acctest.Client(t)
	a := config(project.ID, "127.0.0.1:9100", "tfacc")
	b := config(project.ID, "127.0.0.1:9200", "updated")
	checkReset := func(_ *terraform.State) error {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		s, err := client.ReadPrometheusConfig(ctx, c, project.ID)
		if err != nil {
			return err
		}
		if len(s.Configs) != 0 {
			return fmt.Errorf("discovery groups retained after destroy while parent exists")
		}
		return nil
	}
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(provider.Registry{Resources: []func() frameworkresource.Resource{NewResource}}), CheckDestroy: func(_ *terraform.State) error {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		s, e := client.ReadPrometheusConfig(ctx, c, project.ID)
		if client.IsNotFound(e, "projectNotFound") {
			return nil
		}
		if e != nil {
			return e
		}
		if len(s.Configs) != 0 {
			return fmt.Errorf("discovery groups retained after destroy")
		}
		return nil
	}, Steps: []resource.TestStep{
		{Config: a, Check: resource.TestCheckResourceAttr("zerops_project_prometheus_config.test", "id", project.ID)},
		{Config: a, PlanOnly: true},
		{Config: b, Check: resource.TestCheckResourceAttr("zerops_project_prometheus_config.test", "configs.0.targets.0", "127.0.0.1:9200")},
		{Config: b, PlanOnly: true},
		{ResourceName: "zerops_project_prometheus_config.test", ImportState: true, ImportStateId: project.ID, ImportStateVerify: true},
		{PreConfig: func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			p, e := c.SetPrometheusConfig(ctx, project.ID, []client.PrometheusConfig{{Targets: []string{"127.0.0.1:9300"}, Labels: map[string]string{"job": "outside"}}})
			if e != nil {
				t.Fatal(e)
			}
			if p != nil {
				if _, e = waiter.WaitProcess(ctx, c, p.ID, waiter.WithIntervals(5*time.Second, 5*time.Second, 5*time.Second, time.Minute)); e != nil {
					t.Fatal(e)
				}
			}
		}, Config: b, PlanOnly: true, ExpectNonEmptyPlan: true},
		{Config: b},
		{Config: config(project.ID, "localhost:70000", "invalid"), PlanOnly: true, ExpectError: regexp.MustCompile("Invalid Prometheus target")},
		{Config: b},
		{Config: acctest.ProviderConfig(), Check: checkReset},
		{Config: b},
		{PreConfig: func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
			defer cancel()
			if err := acctest.DeleteTestProject(ctx, c, project.ID); err != nil {
				t.Fatal(err)
			}
		}, RefreshState: true, ExpectNonEmptyPlan: true, Check: func(s *terraform.State) error {
			if _, ok := s.RootModule().Resources["zerops_project_prometheus_config.test"]; ok {
				return fmt.Errorf("absent project remains")
			}
			return nil
		}},
	}})
}
