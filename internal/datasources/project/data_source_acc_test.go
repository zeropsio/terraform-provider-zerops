package project_test

import (
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/zeropsio/terraform-provider-zerops/internal/acctest"
	subject "github.com/zeropsio/terraform-provider-zerops/internal/datasources/project"
	"github.com/zeropsio/terraform-provider-zerops/internal/provider"
	"os"
	"testing"
)

func TestAccProject(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC not set")
	}
	acctest.PreCheck(t)
	p := acctest.CreateTestProject(t)
	resource.Test(t, resource.TestCase{PreCheck: func() { acctest.PreCheck(t) }, ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(provider.Registry{DataSources: []func() datasource.DataSource{subject.NewDataSource}}), Steps: []resource.TestStep{{Config: acctest.ProviderConfig() + fmt.Sprintf(`data "zerops_project" "test" { id = %q }`, p.ID), Check: resource.TestCheckResourceAttr("data.zerops_project.test", "name", p.Name)}, {Config: acctest.ProviderConfig() + fmt.Sprintf(`data "zerops_project" "test" { name = %q }`, p.Name), Check: resource.TestCheckResourceAttr("data.zerops_project.test", "id", p.ID)}}})
}
