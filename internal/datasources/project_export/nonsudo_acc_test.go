package project_export_test

import (
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/zeropsio/terraform-provider-zerops/internal/acctest"
	subject "github.com/zeropsio/terraform-provider-zerops/internal/datasources/project_export"
	"github.com/zeropsio/terraform-provider-zerops/internal/provider"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestAccProjectExportNonSudo(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC not set")
	}
	acctest.PreCheck(t)
	token := os.Getenv("ZEROPS_ACC_NONSUDO_TOKEN")
	if token == "" {
		t.Skip("ZEROPS_ACC_NONSUDO_TOKEN not set; non-sudo reveal rejection needs a separate non-sudo credential")
	}
	p := acctest.CreateTestProject(t)
	config := strings.Replace(acctest.ProviderConfig(), `provider "zerops" {`, `provider "zerops" {`+"\n token = "+strconv.Quote(token), 1)
	resource.Test(t, resource.TestCase{PreCheck: func() { acctest.PreCheck(t) }, ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(provider.Registry{DataSources: []func() datasource.DataSource{subject.NewDataSource}}), Steps: []resource.TestStep{{Config: config + fmt.Sprintf("data \"zerops_project_export\" \"test\" {\n project_id = %q\n reveal = true\n}\n", p.ID), ExpectError: regexp.MustCompile("403")}}})
}
