package service

import (
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/zeropsio/terraform-provider-zerops/internal/acctest"
	"github.com/zeropsio/terraform-provider-zerops/internal/provider"
	"os"
	"regexp"
	"testing"
)

func TestAccServiceLookup(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC is not set")
	}
	acctest.PreCheck(t)
	project := acctest.CreateTestProject(t)
	s := acctest.DeployHTTPApp(t, project.ID)
	factories := acctest.ProtoV6ProviderFactories(provider.Registry{DataSources: []func() datasource.DataSource{NewDataSource}})
	resource.Test(t, resource.TestCase{PreCheck: func() { acctest.PreCheck(t) }, ProtoV6ProviderFactories: factories, Steps: []resource.TestStep{
		{Config: acctest.ProviderConfig() + fmt.Sprintf(`data "zerops_service" "id" { id = %q }
data "zerops_service" "name" {
 project_id = %q
 hostname = %q
}
`, s.ID, project.ID, s.Name), Check: resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr("data.zerops_service.id", "id", s.ID), resource.TestCheckResourceAttr("data.zerops_service.name", "id", s.ID), resource.TestCheckResourceAttr("data.zerops_service.id", "ports.#", "2"), resource.TestCheckResourceAttr("data.zerops_service.id", "ports.0.protocol", "tcp"), resource.TestCheckResourceAttr("data.zerops_service.id", "ports.0.port", "3000"), resource.TestCheckResourceAttrSet("data.zerops_service.id", "active_app_version_id"))},
		{Config: acctest.ProviderConfig() + fmt.Sprintf(`data "zerops_service" "invalid" {
 id = %q
 project_id = %q
 hostname = %q
}`, s.ID, project.ID, s.Name), ExpectError: regexp.MustCompile("Invalid service selector")},
		{Config: acctest.ProviderConfig() + fmt.Sprintf(`data "zerops_service" "missing" {
 project_id = %q
 hostname = "nosuchservice"
}`, project.ID), ExpectError: regexp.MustCompile("Looking up service")},
	}})
}
