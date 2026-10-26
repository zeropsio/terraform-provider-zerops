package project_export_test

import (
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/zeropsio/terraform-provider-zerops/internal/acctest"
	subject "github.com/zeropsio/terraform-provider-zerops/internal/datasources/project_export"
	"github.com/zeropsio/terraform-provider-zerops/internal/provider"
	"os"
	"testing"
)

func TestAccProjectExport(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC not set")
	}
	acctest.PreCheck(t)
	p := acctest.CreateTestProject(t)
	resource.Test(t, resource.TestCase{PreCheck: func() { acctest.PreCheck(t) }, ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(provider.Registry{DataSources: []func() datasource.DataSource{subject.NewDataSource}}), Steps: []resource.TestStep{{Config: acctest.ProviderConfig() + fmt.Sprintf(`data "zerops_project_export" "test" { project_id = %q }`, p.ID), Check: resource.TestCheckResourceAttrWith("data.zerops_project_export.test", "yaml", func(v string) error {
		if v == "" {
			return fmt.Errorf("empty YAML")
		}
		return nil
	})}, {Config: acctest.ProviderConfig() + fmt.Sprintf("data \"zerops_project_export\" \"test\" {\n project_id = %q\n reveal = true\n}\n", p.ID), Check: resource.TestCheckResourceAttrSet("data.zerops_project_export.test", "yaml")}}})
}
