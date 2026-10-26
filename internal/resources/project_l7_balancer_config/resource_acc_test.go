package project_l7_balancer_config

import (
	"context"
	"fmt"
	frameworkresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/zeropsio/terraform-provider-zerops/internal/acctest"
	"github.com/zeropsio/terraform-provider-zerops/internal/provider"
	"github.com/zeropsio/terraform-provider-zerops/internal/waiter"
	"os"
	"testing"
	"time"
)

func balancerConfig(id, values string) string {
	return acctest.ProviderConfig() + fmt.Sprintf(`
resource "zerops_project_l7_balancer_config" "test" {
 project_id = %q
 values = %s
}
`, id, values)
}
func TestAccL7Balancer(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC is not set")
	}
	acctest.PreCheck(t)
	p := acctest.CreateTestProject(t)
	c := acctest.Client(t)
	first := balancerConfig(p.ID, `{client_max_body_size="64m", keepalive_timeout="35"}`)
	second := balancerConfig(p.ID, `{keepalive_timeout="35"}`)
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(provider.Registry{Resources: []func() frameworkresource.Resource{NewResource}}), CheckDestroy: func(_ *terraform.State) error {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		got, e := c.GetL7BalancerConfig(ctx, p.ID)
		if e != nil {
			return e
		}
		for _, v := range got.Values {
			if v.Current != nil {
				return fmt.Errorf("override remains after destroy")
			}
		}
		return nil
	}, Steps: []resource.TestStep{
		{Config: first, Check: resource.TestCheckResourceAttr("zerops_project_l7_balancer_config.test", "values.client_max_body_size", "64m")},
		{Config: first, PlanOnly: true},
		{Config: second, Check: func(_ *terraform.State) error {
			got, e := c.GetL7BalancerConfig(context.Background(), p.ID)
			if e != nil {
				return e
			}
			for _, v := range got.Values {
				if v.Name == "client_max_body_size" && v.Current != nil {
					return fmt.Errorf("omitted override did not reset")
				}
			}
			return nil
		}},
		{ResourceName: "zerops_project_l7_balancer_config.test", ImportState: true, ImportStateVerify: true},
		{Config: second, PlanOnly: true},
		{PreConfig: func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
			defer cancel()
			v := "36"
			process, e := c.SetL7BalancerConfig(ctx, p.ID, map[string]*string{"keepalive_timeout": &v})
			if e != nil {
				t.Fatal(e)
			}
			if _, e = waiter.WaitProcess(ctx, c, process.ID, waiter.WithIntervals(5*time.Second, 5*time.Second, 5*time.Second, time.Minute)); e != nil {
				t.Fatal(e)
			}
		}, Config: second, PlanOnly: true, ExpectNonEmptyPlan: true},
		{Config: second},
		{Config: balancerConfig(p.ID, `{}`)},
	}})
}
