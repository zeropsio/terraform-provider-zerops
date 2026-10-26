package service_types_test

import (
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/zeropsio/terraform-provider-zerops/internal/acctest"
	subject "github.com/zeropsio/terraform-provider-zerops/internal/datasources/service_types"
	"github.com/zeropsio/terraform-provider-zerops/internal/provider"
	"os"
	"strings"
	"testing"
)

func TestAccServiceTypes(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC not set")
	}
	acctest.PreCheck(t)

	resource.Test(t, resource.TestCase{PreCheck: func() { acctest.PreCheck(t) }, ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(provider.Registry{DataSources: []func() datasource.DataSource{subject.NewDataSource}}), Steps: []resource.TestStep{{Config: acctest.ProviderConfig() + `data "zerops_service_types" "test" {}`, Check: func(s *terraform.State) error {
		for _, m := range s.Modules {
			for _, r := range m.Resources {
				for k, v := range r.Primary.Attributes {
					if strings.Contains(k, ".versions.") && strings.HasSuffix(k, ".name") && strings.Contains(strings.ToLower(v), "nodejs") {
						return nil
					}
				}
			}
		}
		return fmt.Errorf("no nodejs version in catalog")
	}}}})
}
