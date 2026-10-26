package project

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/zeropsio/terraform-provider-zerops/internal/acctest"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/provider"
)

func TestMapRetainsCredit(t *testing.T) {
	for _, v := range []types.String{types.StringValue("12.50"), types.StringNull(), types.StringUnknown()} {
		m := Model{MaxCreditLimit: v}
		d := Map(context.Background(), &m, client.Project{ID: "p", Created: time.Now()}, []client.Env{{Type: "SYSTEM", Key: "envIsolation", Content: "none"}})
		if d.HasError() {
			t.Fatal(d)
		}
		if v.IsUnknown() {
			if !m.MaxCreditLimit.IsNull() {
				t.Fatal("unknown credit not null")
			}
		} else if !m.MaxCreditLimit.Equal(v) {
			t.Fatal("credit overwritten")
		}
		if m.EnvIsolation.ValueString() != "none" {
			t.Fatal("isolation not mapped")
		}
	}
}
func TestProjectUpdateHint(t *testing.T) {
	for _, tc := range []struct {
		status     int
		code       string
		null, want bool
	}{{500, "500", true, true}, {500, "500", false, false}, {500, "internalServerError", true, false}, {400, "500", true, false}} {
		e := &client.APIError{HTTPStatus: tc.status, Code: tc.code}
		got := strings.Contains(ProjectUpdateError(e, tc.null), "set max_credit_limit")
		if got != tc.want {
			t.Fatalf("%+v got %v", tc, got)
		}
	}
}
func projectConfig(name, extra string) string {
	return acctest.ProviderConfig() + fmt.Sprintf("resource \"zerops_project\" \"test\" {\n name = %q\n%s\n}\n", name, extra)
}
func projectFactories() provider.Registry {
	return provider.Registry{Resources: []func() fwresource.Resource{NewResource}}
}
func TestAccProjectLifecycle(t *testing.T) {
	name := acctest.RandomName("tfacc")
	var id string
	systemIDs := map[string]string{}
	check := func(cap string) resource.TestCheckFunc {
		return resource.ComposeAggregateTestCheckFunc(resource.TestCheckResourceAttr("zerops_project.test", "status", "ACTIVE"), resource.TestCheckResourceAttr("zerops_project.test", "name", name), func(s *terraform.State) error {
			v := s.RootModule().Resources["zerops_project.test"].Primary
			if id != "" && v.ID != id {
				return fmt.Errorf("project replaced unexpectedly")
			}
			id = v.ID
			if cap != "" && v.Attributes["max_credit_limit"] != cap {
				return fmt.Errorf("credit mismatch")
			}
			rows, err := acctest.Client(t).ListProjectEnv(context.Background(), v.ID)
			if err != nil {
				return err
			}
			for _, row := range rows {
				if row.Type == "SYSTEM" && (row.Key == "envIsolation" || row.Key == "sshIsolation") {
					if prior := systemIDs[row.Key]; prior != "" && prior != row.ID {
						return fmt.Errorf("SYSTEM isolation row ID changed")
					}
					systemIDs[row.Key] = row.ID
				}
			}
			return nil
		})
	}
	mode := "SERIOUS"
	if acctest.IsProd() {
		mode = "LIGHT"
		t.Log("production gentleness: keep applied project mode LIGHT; skip SERIOUS-to-LIGHT replacement plan because it requires a prior SERIOUS apply")
	}
	steps := []resource.TestStep{
		{Config: projectConfig(name, ""), Check: check("")},
		{Config: projectConfig(name+"-renamed", `description = "first"
 tags = ["alpha", "beta"]
 public_ipv4_shared = true`)},
		{Config: projectConfig(name, `description = "second"
 tags = ["beta"]
 max_credit_limit = "25"`), Check: check("25")},
		{Config: projectConfig(name, `description = "second"
 tags = ["beta"]
 max_credit_limit = "30"`), Check: check("30")},
		{Config: projectConfig(name, `description = "second"
 tags = ["beta"]`), Check: check("30")},
		{Config: projectConfig(name, fmt.Sprintf(`description = "third"
 env_isolation = "none"
 ssh_isolation = "vpn"
 mode = %q`, mode)), Check: resource.ComposeAggregateTestCheckFunc(check("30"), resource.TestCheckResourceAttr("zerops_project.test", "mode", mode), resource.TestCheckResourceAttr("zerops_project.test", "env_isolation", "none"))},
		{ResourceName: "zerops_project.test", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"max_credit_limit"}},
		{Config: projectConfig(name, `description = "third"
 env_isolation = "none"
 ssh_isolation = "vpn"
 mode = "LIGHT"`), PlanOnly: true, ExpectNonEmptyPlan: true, ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectResourceAction("zerops_project.test", plancheck.ResourceActionReplace)}}},
		{Config: projectConfig(name, fmt.Sprintf("mode = %q\nlocation = \"other-location\"", mode)), PlanOnly: true, ExpectNonEmptyPlan: true, ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectResourceAction("zerops_project.test", plancheck.ResourceActionReplace)}}},
		{Config: projectConfig(name, fmt.Sprintf("mode = %q\norganization_id = \"other-organization\"", mode)), PlanOnly: true, ExpectNonEmptyPlan: true, ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectResourceAction("zerops_project.test", plancheck.ResourceActionReplace)}}},
	}
	if acctest.IsProd() {
		// This plan requires SERIOUS state, which production must never create.
		steps = append(steps[:7], steps[8:]...)
	}
	resource.Test(t, resource.TestCase{PreCheck: func() { acctest.PreCheck(t) }, ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(projectFactories()), Steps: steps})
}
func TestAccProjectDisappears(t *testing.T) {
	name := acctest.RandomName("tfacc")
	resource.Test(t, resource.TestCase{PreCheck: func() { acctest.PreCheck(t) }, ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(projectFactories()), Steps: []resource.TestStep{{Config: projectConfig(name, ""), Check: func(s *terraform.State) error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		return acctest.DeleteTestProject(ctx, acctest.Client(t), s.RootModule().Resources["zerops_project.test"].Primary.ID)
	}, ExpectNonEmptyPlan: true}}})
}
func TestAccProjectExternalCreditDiagnostic(t *testing.T) {
	name := acctest.RandomName("tfacc")
	resource.Test(t, resource.TestCase{PreCheck: func() { acctest.PreCheck(t) }, ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(projectFactories()), Steps: []resource.TestStep{{Config: projectConfig(name, ""), Check: func(s *terraform.State) error {
		cap := 20.0
		_, e := acctest.Client(t).UpdateProject(context.Background(), s.RootModule().Resources["zerops_project.test"].Primary.ID, client.ProjectUpdate{Name: name, TagList: []string{}, MaxCreditLimit: &cap})
		return e
	}}, {Config: projectConfig(name+"-rename", ""), ExpectError: regexp.MustCompile("project probably has a credit limit set outside Terraform")}}})
}
