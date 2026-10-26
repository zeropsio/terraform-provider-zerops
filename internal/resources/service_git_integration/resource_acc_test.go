package service_git_integration_test

import "github.com/hashicorp/terraform-plugin-testing/terraform"
import (
	"context"
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	tfresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/zeropsio/terraform-provider-zerops/internal/acctest"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/locks"
	"github.com/zeropsio/terraform-provider-zerops/internal/provider"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
	integration "github.com/zeropsio/terraform-provider-zerops/internal/resources/service_git_integration"
	"github.com/zeropsio/terraform-provider-zerops/internal/waiter"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAccServiceGitIntegrationOAuth(t *testing.T) {
	if os.Getenv("TF_ACC") == "" || os.Getenv("ZEROPS_ACC_GIT_OAUTH") != "1" {
		t.Skip("requires explicit ZEROPS_ACC_GIT_OAUTH=1 and OAuth consent for this bearer identity")
	}
	if !acctest.IsProd() {
		t.Skip("OAuth connector acceptance requires a pre-authorized production identity")
	}
	gitProvider := os.Getenv("ZEROPS_ACC_GIT_PROVIDER")
	repository := os.Getenv("ZEROPS_ACC_GIT_REPOSITORY")
	branch := os.Getenv("ZEROPS_ACC_GIT_BRANCH")
	if err := validateProductionOAuthFixture(gitProvider, repository, branch, os.Getenv("ZEROPS_ACC_PROD_GIT_REPOSITORY")); err != nil {
		t.Fatal(err)
	}
	acctest.PreCheck(t)
	p := acctest.CreateTestProject(t)
	c := acctest.Client(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	start := false
	s, err := c.CreateService(ctx, p.ID, acctest.MinimalServiceInput(client.ServiceCreate{Name: acctest.RandomHostname(), ServiceStackVersionName: "alpine/nodejs@22", StartWithoutCode: &start}))
	if err != nil {
		t.Fatal(err)
	}
	if s.Process != nil {
		if _, err = waiter.WaitProcess(ctx, c, s.Process.ID); err != nil {
			t.Fatal(err)
		}
	}
	config := acctest.ProviderConfig() + fmt.Sprintf(`resource "zerops_service_git_integration" "test" {
 service_id = %q
 git_provider = %q
 repository_full_name = %q
 event_type = "BRANCH"
 branch_name = %q
 zerops_yaml_setup = "app"
 }`, s.ID, gitProvider, repository, branch)
	tfresource.Test(t, tfresource.TestCase{ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(provider.Registry{Resources: []func() resource.Resource{integration.NewResource}}), CheckDestroy: func(_ *terraform.State) error {
		_, err := c.GetGitIntegration(ctx, s.ID)
		if !client.IsNotFound(err, "noExternalRepositoryIntegration") {
			return fmt.Errorf("connector cleanup did not prove absence: %v", err)
		}
		processes, err := c.AllServiceProcesses(ctx, s.ID)
		if err != nil {
			return err
		}
		for _, process := range processes {
			action := strings.ToLower(process.ActionName)
			if strings.Contains(action, "build") || strings.Contains(action, "deploy") {
				return fmt.Errorf("connector reconciliation unexpectedly created build/deploy process %s", process.ID)
			}
		}
		return nil
	}, Steps: []tfresource.TestStep{{Config: config}, {ResourceName: "zerops_service_git_integration.test", ImportState: true, ImportStateVerify: true}, {Config: config, PlanOnly: true}, {Config: strings.Replace(config, fmt.Sprintf("branch_name = %q", branch), fmt.Sprintf("branch_name = %q\n is_active = false", branch), 1)}}})
	if t.Failed() {
		t.Fatal("ordinary connector cleanup must pass before parent disappearance exercise")
	}
	// Ordinary CLI destroy above must first prove connector/webhook absence and
	// no build/deploy work. Reuse its dormant service to exercise the guarded
	// framework Delete path after external parent disappearance, without a
	// second project, connector, or CLI destruction.
	deleteResource := integration.NewResource()
	var configured resource.ConfigureResponse
	deleteResource.(resource.ResourceWithConfigure).Configure(ctx, resource.ConfigureRequest{ProviderData: &providerdata.ProviderData{API: c, Locks: &locks.Keyed{}}}, &configured)
	if configured.Diagnostics.HasError() {
		t.Fatal(configured.Diagnostics)
	}
	deleteState := frameworkDeleteState(t, ctx, deleteResource, s.ID)
	process, err := c.DeleteService(ctx, s.ID)
	if err != nil {
		t.Fatalf("deleting same dormant service fixture: %v", err)
	}
	if _, err = waiter.WaitProcess(ctx, c, process.ID); err != nil {
		t.Fatal(err)
	}
	if err = waiter.WaitNotFound(ctx, s.ID, func(ctx context.Context) error {
		_, err := c.GetService(ctx, s.ID)
		return err
	}, []string{"serviceStackNotFound"}); err != nil {
		t.Fatal(err)
	}
	deleted := resource.DeleteResponse{State: deleteState}
	deleteResource.Delete(ctx, resource.DeleteRequest{State: deleteState}, &deleted)
	if deleted.Diagnostics.HasError() {
		t.Fatalf("guarded framework Delete after same service disappearance: %v", deleted.Diagnostics)
	}
}

func frameworkDeleteState(t *testing.T, ctx context.Context, r resource.Resource, serviceID string) tfsdk.State {
	t.Helper()
	var schema resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schema)
	if schema.Diagnostics.HasError() {
		t.Fatal(schema.Diagnostics)
	}
	typ := schema.Schema.Type().TerraformType(ctx)
	values := make(map[string]tftypes.Value)
	for name, attrType := range typ.(tftypes.Object).AttributeTypes {
		values[name] = tftypes.NewValue(attrType, nil)
	}
	imported := resource.ImportStateResponse{State: tfsdk.State{
		Schema: schema.Schema,
		Raw:    tftypes.NewValue(typ, values),
	}}
	r.(resource.ResourceWithImportState).ImportState(ctx, resource.ImportStateRequest{ID: serviceID}, &imported)
	if imported.Diagnostics.HasError() {
		t.Fatal(imported.Diagnostics)
	}
	return imported.State
}

func TestFrameworkDeleteStateImport(t *testing.T) {
	ctx := context.Background()
	state := frameworkDeleteState(t, ctx, integration.NewResource(), "saved-service")
	for _, name := range []string{"id", "service_id"} {
		var got types.String
		if d := state.GetAttribute(ctx, path.Root(name), &got); d.HasError() {
			t.Fatal(d)
		}
		if got.ValueString() != "saved-service" {
			t.Fatalf("%s = %v; want saved-service", name, got)
		}
	}
}

// pinned is the only GitHub fixture repository (owner/name) production OAuth
// acceptance may connect, supplied via ZEROPS_ACC_PROD_GIT_REPOSITORY.
func validateProductionOAuthFixture(gitProvider, repository, branch, pinned string) error {
	if pinned == "" || strings.Count(pinned, "/") != 1 || strings.TrimSpace(pinned) != pinned {
		return fmt.Errorf("production OAuth acceptance requires ZEROPS_ACC_PROD_GIT_REPOSITORY=<owner>/<repository>")
	}
	if gitProvider != "github" || repository != pinned {
		return fmt.Errorf("production OAuth acceptance requires explicit ZEROPS_ACC_GIT_PROVIDER=github and ZEROPS_ACC_GIT_REPOSITORY=%s", pinned)
	}
	if branch == "" || branch == "main" || branch == "HEAD" || branch == "refs/heads/main" || strings.TrimSpace(branch) != branch {
		return fmt.Errorf("production OAuth acceptance requires explicit ZEROPS_ACC_GIT_BRANCH naming a non-main fixture branch")
	}
	return nil
}

func TestProductionOAuthFixture(t *testing.T) {
	const repository = "zerops-test/fixture"
	for _, tc := range []struct {
		name       string
		provider   string
		repository string
		branch     string
		ok         bool
		unpinned   bool // leaves ZEROPS_ACC_PROD_GIT_REPOSITORY empty
	}{
		{name: "authorized", provider: "github", repository: repository, branch: "tfprobe/acceptance", ok: true},
		{name: "missing provider", repository: repository, branch: "tfprobe/acceptance"},
		{name: "gitlab", provider: "gitlab", repository: repository, branch: "tfprobe/acceptance"},
		{name: "missing repository", provider: "github", branch: "tfprobe/acceptance"},
		{name: "other repository", provider: "github", repository: "zerops-test/other", branch: "tfprobe/acceptance"},
		{name: "url instead of full name", provider: "github", repository: "https://github.com/" + repository, branch: "tfprobe/acceptance"},
		{name: "missing branch", provider: "github", repository: repository},
		{name: "blank branch", provider: "github", repository: repository, branch: " "},
		{name: "main", provider: "github", repository: repository, branch: "main"},
		{name: "qualified main", provider: "github", repository: repository, branch: "refs/heads/main"},
		{name: "padded main", provider: "github", repository: repository, branch: " main "},
		{name: "default head", provider: "github", repository: repository, branch: "HEAD"},
		{name: "no pinned repository", provider: "github", repository: repository, branch: "tfprobe/acceptance", unpinned: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pin := repository
			if tc.unpinned {
				pin = ""
			}
			if err := validateProductionOAuthFixture(tc.provider, tc.repository, tc.branch, pin); (err == nil) != tc.ok {
				t.Fatalf("fixture validation error = %v, want accepted = %t", err, tc.ok)
			}
		})
	}
}
