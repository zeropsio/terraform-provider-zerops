package service_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/zeropsio/terraform-provider-zerops/internal/acctest"
)

func TestAccServicePostgresqlDiskFloor(t *testing.T) {
	project := accProject(t)
	name := acctest.RandomHostname()
	cfg := func(extra string) string {
		return acctest.ProviderConfig() + fmt.Sprintf(`
resource "zerops_service" "test" {
 project_id = %q
 hostname = %q
 type = "postgresql:single@16"
 %s
}
`, project, name, extra)
	}
	defaultConfig := cfg("")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: factories(),
		Steps: []resource.TestStep{
			{Config: cfg(`vertical_autoscaling = { min_disk_gb = 1 }`), ExpectError: regexp.MustCompile(`PostgreSQL disk below provider floor`)},
			{Config: defaultConfig, Check: resource.TestCheckResourceAttr("zerops_service.test", "vertical_autoscaling.min_disk_gb", "2")},
			{Config: defaultConfig, PlanOnly: true, ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPreRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}},
		},
	})
}
