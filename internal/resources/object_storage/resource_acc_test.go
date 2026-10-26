package object_storage

import (
	"context"
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	testingresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/zeropsio/terraform-provider-zerops/internal/acctest"
	"github.com/zeropsio/terraform-provider-zerops/internal/provider"
	"os"
	"regexp"
	"testing"
)

func TestAccObjectStorageCDN(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC required")
	}
	acctest.PreCheck(t)
	if acctest.IsProd() && os.Getenv("ZEROPS_ACC_CDN_ENABLE") != "1" {
		t.Skip("production enable requires ZEROPS_ACC_CDN_ENABLE=1")
	}
	project := acctest.CreateTestProject(t)
	api := acctest.Client(t)
	host := acctest.RandomHostname()
	config := func(enabled bool, quota int) string {
		if acctest.IsProd() {
			quota = 1
		}
		return acctest.ProviderConfig() + fmt.Sprintf("resource \"zerops_object_storage\" \"test\" {\nproject_id=%q\nhostname=%q\nquota_gb=%d\ncdn_enabled=%t\n}\n", project.ID, host, quota, enabled)
	}
	var id string
	check := func(enabled bool) testingresource.TestCheckFunc {
		return func(s *terraform.State) error {
			row := s.RootModule().Resources["zerops_object_storage.test"].Primary
			if row.ID == "" {
				return fmt.Errorf("bucket identity lost")
			}
			if id != "" && id != row.ID {
				return fmt.Errorf("bucket unexpectedly replaced")
			}
			id = row.ID
			v, e := api.GetService(context.Background(), id)
			if e != nil {
				return e
			}
			if v.CDNEnabled == nil || *v.CDNEnabled != enabled {
				return fmt.Errorf("unexpected remote CDN flag")
			}
			if row.Attributes["cdn_enabled"] != fmt.Sprint(enabled) {
				return fmt.Errorf("state CDN flag differs from remote")
			}
			return nil
		}
	}
	enable := testingresource.TestStep{Config: config(true, 1)}
	if acctest.IsProd() {
		enable.Check = check(true)
	} else {
		enable.ExpectError = regexp.MustCompile(`(?s)stack.enableCdnStorage.*FAILED.*internalServerError`)
	}
	steps := []testingresource.TestStep{{Config: config(false, 1), Check: check(false)}, enable}
	if acctest.IsProd() {
		steps = append(steps, testingresource.TestStep{Config: config(true, 2), Check: check(true)}, testingresource.TestStep{ResourceName: "zerops_object_storage.test", ImportState: true, ImportStateVerify: true})
	}
	steps = append(steps, testingresource.TestStep{Config: config(false, 1), Check: check(false)}, testingresource.TestStep{Config: config(false, 1), PlanOnly: true})
	testingresource.Test(t, testingresource.TestCase{ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(provider.Registry{Resources: []func() resource.Resource{NewResource}}), Steps: steps})
}
