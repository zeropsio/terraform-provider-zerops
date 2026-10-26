package projects_test

import (
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/zeropsio/terraform-provider-zerops/internal/acctest"
	subject "github.com/zeropsio/terraform-provider-zerops/internal/datasources/projects"
	"github.com/zeropsio/terraform-provider-zerops/internal/provider"
	"os"
	"testing"
)

func TestAccProjects(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC not set")
	}
	acctest.PreCheck(t)
	p := acctest.CreateTestProject(t)
	resource.Test(t, resource.TestCase{PreCheck: func() { acctest.PreCheck(t) }, ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(provider.Registry{DataSources: []func() datasource.DataSource{subject.NewDataSource}}), Steps: []resource.TestStep{{Config: acctest.ProviderConfig() + fmt.Sprintf("data \"zerops_projects\" \"test\" {\n name_contains = %q\n statuses = [\"ACTIVE\"]\n}\n", p.Name), Check: resource.ComposeAggregateTestCheckFunc(resource.TestCheckResourceAttr("data.zerops_projects.test", "projects.#", "1"), resource.TestCheckResourceAttr("data.zerops_projects.test", "projects.0.id", p.ID))}}})
}
