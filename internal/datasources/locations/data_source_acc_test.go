package locations_test

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/zeropsio/terraform-provider-zerops/internal/acctest"
	subject "github.com/zeropsio/terraform-provider-zerops/internal/datasources/locations"
	"github.com/zeropsio/terraform-provider-zerops/internal/provider"
)

func TestAccLocations(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC not set")
	}
	acctest.PreCheck(t)
	locationID := "development"
	if acctest.IsProd() {
		locationID = "eu-central"
	}
	resource.Test(t, resource.TestCase{PreCheck: func() { acctest.PreCheck(t) }, ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(provider.Registry{DataSources: []func() datasource.DataSource{subject.NewDataSource}}), Steps: []resource.TestStep{{Config: acctest.ProviderConfig() + `data "zerops_locations" "test" {}`, Check: resource.TestCheckTypeSetElemNestedAttrs("data.zerops_locations.test", "locations.*", map[string]string{"id": locationID})}}})
}
