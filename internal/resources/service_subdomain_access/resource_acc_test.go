package service_subdomain_access

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

func accessConfig(id string) string {
	return acctest.ProviderConfig() + fmt.Sprintf(`
resource "zerops_service_subdomain_access" "test" {
 service_id = %q
}
`, id)
}
func TestAccSubdomainAccess(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC is not set")
	}
	acctest.PreCheck(t)
	project := acctest.CreateTestProject(t)
	service := acctest.DeployHTTPApp(t, project.ID)
	c := acctest.Client(t)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(provider.Registry{Resources: []func() frameworkresource.Resource{NewResource}}),
		CheckDestroy: func(_ *terraform.State) error {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			got, err := c.GetService(ctx, service.ID)
			if err != nil {
				return err
			}
			if got.SubdomainAccess {
				return fmt.Errorf("subdomain access remains enabled after destroy")
			}
			return nil
		},
		Steps: []resource.TestStep{
			{Config: accessConfig(service.ID), Check: resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr("zerops_service_subdomain_access.test", "id", service.ID), resource.TestCheckResourceAttrWith("zerops_service_subdomain_access.test", "urls.#", func(value string) error {
				if value == "0" {
					return fmt.Errorf("no generated URLs")
				}
				return nil
			}))},
			{ResourceName: "zerops_service_subdomain_access.test", ImportState: true, ImportStateVerify: true},
			{PreConfig: func() {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
				defer cancel()
				p, err := c.SetSubdomainAccess(ctx, service.ID, false)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = waiter.WaitProcess(ctx, c, p.ID); err != nil {
					t.Fatal(err)
				}
			}, RefreshState: true, Check: func(state *terraform.State) error {
				if _, ok := state.RootModule().Resources["zerops_service_subdomain_access.test"]; ok {
					return fmt.Errorf("disabled access remains in state")
				}
				return nil
			}, ExpectNonEmptyPlan: true},
			{Config: accessConfig(service.ID)},
		},
	})
}
func TestAccSubdomainAccessNoHTTPPorts(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC is not set")
	}
	acctest.PreCheck(t)
	project := acctest.CreateTestProject(t)
	c := acctest.Client(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	start := true
	service, err := c.CreateService(ctx, project.ID, acctest.MinimalServiceInput(client.ServiceCreate{Name: acctest.RandomHostname(), ServiceStackVersionName: "alpine/nodejs@22", StartWithoutCode: &start}))
	if err != nil {
		t.Fatal(err)
	}
	if service.Process != nil {
		if _, err = waiter.WaitProcess(ctx, c, service.Process.ID); err != nil {
			t.Fatal(err)
		}
	}
	baseline, err := waiter.SnapshotProcessIDs(ctx, c, service.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = waiter.WaitServiceSettled(ctx, c, service.ID, []string{"ACTIVE"}, baseline, nil); err != nil {
		t.Fatal(err)
	}
	resource.Test(t, resource.TestCase{PreCheck: func() { acctest.PreCheck(t) }, ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(provider.Registry{Resources: []func() frameworkresource.Resource{NewResource}}), Steps: []resource.TestStep{{Config: accessConfig(service.ID), ExpectError: regexp.MustCompile("service has no HTTP ports; deploy first")}}})
}
