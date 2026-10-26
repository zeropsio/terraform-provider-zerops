package project_env

import (
	"context"
	"fmt"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/zeropsio/terraform-provider-zerops/internal/acctest"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/provider"
	"github.com/zeropsio/terraform-provider-zerops/internal/waiter"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestFindUser(t *testing.T) {
	rows := []client.Env{{ID: "sys", Key: "FOO", Type: "SYSTEM"}, {ID: "user", Key: "foo", Type: "USER"}}
	r, e := FindUser(rows, "FoO")
	if e != nil || r.ID != "user" {
		t.Fatal(r, e)
	}
	rows = append(rows, r)
	if _, e = FindUser(rows, "foo"); e == nil {
		t.Fatal("duplicate accepted")
	}
	if _, e = FindUser(rows, "absent"); e == nil {
		t.Fatal("missing accepted")
	}
}
func TestContentValidator(t *testing.T) {
	for _, tc := range []struct {
		s   string
		bad bool
	}{{"ok", false}, {strings.Repeat("x", 8192), false}, {strings.Repeat("x", 8193), true}, {"secret\nvalue", true}, {"é", true}} {
		p := validator.StringResponse{}
		contentValidator{}.ValidateString(context.Background(), validator.StringRequest{ConfigValue: types.StringValue(tc.s)}, &p)
		if p.Diagnostics.HasError() != tc.bad {
			t.Fatalf("content validation mismatch length %d", len(tc.s))
		}
	}
}
func TestReservedKeys(t *testing.T) {
	for _, key := range []string{"envIsolation", "SSHISOLATION"} {
		p := validator.StringResponse{}
		keyValidator{}.ValidateString(context.Background(), validator.StringRequest{ConfigValue: types.StringValue(key)}, &p)
		if !p.Diagnostics.HasError() {
			t.Fatal("reserved key accepted")
		}
	}
}
func factories() provider.Registry {
	return provider.Registry{Resources: []func() fwresource.Resource{NewResource}}
}
func config(project, key, content string, sensitive bool) string {
	return acctest.ProviderConfig() + fmt.Sprintf("resource \"zerops_project_env\" \"test\" {\n project_id = %q\n key = %q\n content = %q\n sensitive = %t\n}\n", project, key, content, sensitive)
}
func TestAccProjectEnvLifecycle(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC required")
	}
	acctest.PreCheck(t)
	project := acctest.CreateTestProject(t).ID
	var id string
	check := func(s *terraform.State) error {
		v := s.RootModule().Resources["zerops_project_env.test"].Primary
		if id != "" && v.ID != id {
			return fmt.Errorf("environment renamed with replacement")
		}
		id = v.ID
		row, e := acctest.Client(t).GetProjectEnv(context.Background(), id)
		if e != nil {
			return e
		}
		if row.Type != "USER" {
			return fmt.Errorf("not a USER row")
		}
		return nil
	}
	resource.Test(t, resource.TestCase{PreCheck: func() { acctest.PreCheck(t) }, ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(factories()), Steps: []resource.TestStep{
		{Config: config(project, "TFACC_ONE", "plain", false), Check: check},
		{Config: config(project, "TFACC_TWO", "secret-two", true), Check: check},
		{ResourceName: "zerops_project_env.test", ImportState: true, ImportStateVerify: true},
		{ResourceName: "zerops_project_env.test", ImportState: true, ImportStateId: project + "/tfacc_two", ImportStateVerify: true},
		{Config: config(project, "TFACC_TWO", "changed", false), Check: check},
		{Config: config("other-project", "TFACC_TWO", "changed", false), PlanOnly: true, ExpectNonEmptyPlan: true, ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectResourceAction("zerops_project_env.test", plancheck.ResourceActionReplace)}}},
	}})
}
func TestAccProjectEnvDisappears(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC required")
	}
	acctest.PreCheck(t)
	project := acctest.CreateTestProject(t).ID
	resource.Test(t, resource.TestCase{PreCheck: func() { acctest.PreCheck(t) }, ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(factories()), Steps: []resource.TestStep{{Config: config(project, "TFACC_DELETE", "value", false), ExpectNonEmptyPlan: true, Check: func(s *terraform.State) error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		api := acctest.Client(t)
		proc, e := api.DeleteProjectEnv(ctx, s.RootModule().Resources["zerops_project_env.test"].Primary.ID)
		if e != nil {
			return e
		}
		_, e = waiter.WaitProcess(ctx, api, proc.ID)
		return e
	}}}})
}
func TestAccProjectEnvNonSudoRefresh(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC required")
	}
	acctest.PreCheck(t)
	if os.Getenv("ZEROPS_ACC_NONSUDO_TOKEN") == "" {
		t.Skip("ZEROPS_ACC_NONSUDO_TOKEN is not configured")
	}
	project := acctest.CreateTestProject(t).ID
	cfg := config(project, "TFACC_SECRET", "known-secret-content", true)
	nonSudo := strings.Replace(cfg, "provider \"zerops\" {", "variable \"nonsudo_token\" {\n type = string\n sensitive = true\n}\nprovider \"zerops\" {\n token = var.nonsudo_token", 1)
	t.Setenv("TF_VAR_nonsudo_token", os.Getenv("ZEROPS_ACC_NONSUDO_TOKEN"))
	resource.Test(t, resource.TestCase{PreCheck: func() { acctest.PreCheck(t) }, ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(factories()), Steps: []resource.TestStep{{Config: cfg}, {Config: nonSudo, PlanOnly: true, Check: resource.TestCheckResourceAttr("zerops_project_env.test", "content", "known-secret-content")}}})
}
func TestAccProjectEnvSystemRejected(t *testing.T) {
	resource.Test(t, resource.TestCase{PreCheck: func() { acctest.PreCheck(t) }, ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(factories()), Steps: []resource.TestStep{{Config: config("unused", "envIsolation", "none", false), ExpectError: regexp.MustCompile("Reserved SYSTEM key")}}})
}
