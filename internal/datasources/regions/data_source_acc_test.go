package regions_test

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/zeropsio/terraform-provider-zerops/internal/acctest"
	subject "github.com/zeropsio/terraform-provider-zerops/internal/datasources/regions"
	"github.com/zeropsio/terraform-provider-zerops/internal/provider"
)

func TestAccRegions(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC not set")
	}
	regionName := os.Getenv("ZEROPS_ACC_REGION")
	if regionName == "" && acctest.IsProd() {
		regionName = "prg1"
	}
	if regionName == "" {
		t.Skip("set ZEROPS_ACC_REGION to the expected development region")
	}
	acctest.PreCheck(t)
	resource.Test(t, resource.TestCase{PreCheck: func() { acctest.PreCheck(t) }, ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(provider.Registry{DataSources: []func() datasource.DataSource{subject.NewDataSource}}), Steps: []resource.TestStep{{Config: acctest.ProviderConfig() + `data "zerops_regions" "test" {}`, Check: resource.TestCheckTypeSetElemNestedAttrs("data.zerops_regions.test", "regions.*", map[string]string{"name": regionName})}}})
}
