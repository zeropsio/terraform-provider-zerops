package project_dedicated_ipv4

import (
	"context"
	"fmt"
	frameworkresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/zeropsio/terraform-provider-zerops/internal/acctest"
	"github.com/zeropsio/terraform-provider-zerops/internal/provider"
	"os"
	"regexp"
	"testing"
)

func TestAccDedicatedIPv4Validation(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC required")
	}
	acctest.PreCheck(t)
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(provider.Registry{Resources: []func() frameworkresource.Resource{NewResource}}), Steps: []resource.TestStep{
		{Config: acctest.ProviderConfig() + `resource "zerops_project_dedicated_ipv4" "test" { project_id = "" }`, PlanOnly: true, ExpectError: regexp.MustCompile("length")},
	}})
}
func TestAccDedicatedIPv4Adopt(t *testing.T) {
	p := acctest.DedicatedIPv4Fixture(t)
	config := acctest.ProviderConfig() + fmt.Sprintf(`resource "zerops_project_dedicated_ipv4" "test" { project_id = %q }`, p.ID)
	retained := func(_ *terraform.State) error {
		got, err := acctest.Client(t).GetProject(context.Background(), p.ID)
		if err != nil {
			return err
		}
		if got.PublicIPv4 == nil || *got.PublicIPv4 != *p.PublicIPv4 {
			return fmt.Errorf("holder IPv4 changed")
		}
		return nil
	}
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: acctest.AdoptOnlyIPv4Factories(provider.Registry{Resources: []func() frameworkresource.Resource{NewResource}}), CheckDestroy: retained, Steps: []resource.TestStep{
		{Config: config, Check: resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr("zerops_project_dedicated_ipv4.test", "address", *p.PublicIPv4), retained)},
		{Config: config, PlanOnly: true},
		{ResourceName: "zerops_project_dedicated_ipv4.test", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"timeouts"}},
		{Config: acctest.ProviderConfig(), Check: retained},
	}})
}
