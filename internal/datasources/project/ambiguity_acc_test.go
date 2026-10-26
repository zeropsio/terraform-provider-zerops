package project_test

import (
	"context"
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/zeropsio/terraform-provider-zerops/internal/acctest"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	subject "github.com/zeropsio/terraform-provider-zerops/internal/datasources/project"
	"github.com/zeropsio/terraform-provider-zerops/internal/provider"
	"os"
	"regexp"
	"testing"
)

func TestAccProjectAmbiguous(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC not set")
	}
	acctest.PreCheck(t)
	first := acctest.CreateTestProject(t)
	second := acctest.CreateTestProject(t)
	credit := 1.0
	_, err := acctest.Client(t).UpdateProject(context.Background(), second.ID, client.ProjectUpdate{Name: first.Name, TagList: []string{}, MaxCreditLimit: &credit})
	if err != nil {
		t.Fatal(err)
	}
	resource.Test(t, resource.TestCase{PreCheck: func() { acctest.PreCheck(t) }, ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(provider.Registry{DataSources: []func() datasource.DataSource{subject.NewDataSource}}), Steps: []resource.TestStep{{Config: acctest.ProviderConfig() + fmt.Sprintf(`data "zerops_project" "test" { name = %q }`, first.Name), ExpectError: regexp.MustCompile("multiple projects")}}})
}
