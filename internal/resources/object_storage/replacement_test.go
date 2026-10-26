package object_storage

import (
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	testingresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/zeropsio/terraform-provider-zerops/internal/acctest"
	"github.com/zeropsio/terraform-provider-zerops/internal/provider"
	"os"
	"testing"
)

func TestAccObjectStorageProjectReplacement(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC required")
	}
	acctest.PreCheck(t)
	first, second := acctest.CreateTestProject(t), acctest.CreateTestProject(t)
	host := acctest.RandomHostname()
	config := func(project string) string {
		return acctest.ProviderConfig() + fmt.Sprintf("resource \"zerops_object_storage\" \"test\" {\nproject_id=%q\nhostname=%q\nquota_gb=1\n}\n", project, host)
	}
	var original string
	testingresource.Test(t, testingresource.TestCase{ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(provider.Registry{Resources: []func() resource.Resource{NewResource}}), Steps: []testingresource.TestStep{
		{Config: config(first.ID), Check: func(state *terraform.State) error {
			original = state.RootModule().Resources["zerops_object_storage.test"].Primary.ID
			return nil
		}},
		{Config: config(second.ID), Check: func(state *terraform.State) error {
			if state.RootModule().Resources["zerops_object_storage.test"].Primary.ID == original {
				return fmt.Errorf("parent change retained storage ID")
			}
			return nil
		}},
	}})
}
