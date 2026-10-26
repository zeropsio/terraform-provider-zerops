package project_remote_logging

import (
	"context"
	"fmt"
	frameworkresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/zeropsio/terraform-provider-zerops/internal/acctest"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/provider"
	"github.com/zeropsio/terraform-provider-zerops/internal/waiter"
	"os"
	"testing"
	"time"
)

func loggingConfig(id, snippet, typ string) string {
	return acctest.ProviderConfig() + fmt.Sprintf(`
resource "zerops_project_remote_logging" "test" {
 project_id = %q
 syslog_config = %q
 %s
}
`, id, snippet, typ)
}
func TestAccRemoteLogging(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC is not set")
	}
	acctest.PreCheck(t)
	p := acctest.CreateTestProject(t)
	c := acctest.Client(t)
	snippet := `destination d_tfacc { file("/dev/null"); }; log { source(s_src); destination(d_tfacc); };`
	config := loggingConfig(p.ID, snippet, `type = "syslog"`)
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(provider.Registry{Resources: []func() frameworkresource.Resource{NewResource}}), CheckDestroy: func(_ *terraform.State) error {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		got, e := c.GetProject(ctx, p.ID)
		if client.IsNotFound(e, "projectNotFound") {
			return nil
		}
		if e != nil {
			return e
		}
		if got.LogForwardingType != nil {
			return fmt.Errorf("logging type not reset")
		}
		return nil
	}, Steps: []resource.TestStep{
		{Config: config, Check: resource.TestCheckResourceAttr("zerops_project_remote_logging.test", "type", "syslog")},
		{Config: config, PlanOnly: true},
		{PreConfig: func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
			defer cancel()
			typ := "syslog"
			process, err := c.SetRemoteLogging(ctx, p.ID, client.RemoteLoggingWrite{SyslogConfig: snippet, Certificate: "", Type: &typ})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = waiter.WaitProcess(ctx, c, process.ID, waiter.WithIntervals(5*time.Second, 5*time.Second, 5*time.Second, time.Minute)); err != nil {
				t.Fatal(err)
			}
		}, Config: config, PlanOnly: true},
		{ResourceName: "zerops_project_remote_logging.test", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"syslog_config", "certificate"}},
		{Config: config},
		{Config: loggingConfig(p.ID, snippet+"\n", `type = "syslog"`)},
		{Config: loggingConfig(p.ID, snippet, "")},
		{Config: loggingConfig(p.ID, "", "")},
		{Config: loggingConfig(p.ID, "", ""), PlanOnly: true},
		{Config: acctest.ProviderConfig(), Check: func(_ *terraform.State) error {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			got, err := c.GetProject(ctx, p.ID)
			if err != nil {
				return err
			}
			if got.LogForwardingType != nil {
				return fmt.Errorf("logging type not reset while parent exists")
			}
			return nil
		}},
		{Config: config},
		{PreConfig: func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
			defer cancel()
			if e := acctest.DeleteTestProject(ctx, c, p.ID); e != nil {
				t.Fatal(e)
			}
		}, RefreshState: true, ExpectNonEmptyPlan: true, Check: func(s *terraform.State) error {
			if _, ok := s.RootModule().Resources["zerops_project_remote_logging.test"]; ok {
				return fmt.Errorf("absent project remains")
			}
			return nil
		}},
	}})
}
