package integration_token

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	frameworkresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/zeropsio/terraform-provider-zerops/internal/acctest"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/locks"
	"github.com/zeropsio/terraform-provider-zerops/internal/provider"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
)

func TestAccIntegrationToken(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC is not set")
	}
	acctest.PreCheck(t)
	cfg, org, err := acctest.Environment()
	if err != nil {
		t.Fatal(err)
	}
	admin := os.Getenv("ZEROPS_ACC_TOKEN_ADMIN_TOKEN")
	if os.Getenv("ZEROPS_ACC_TOKEN_ADMIN") != "1" || admin == "" {
		if acctest.IsProd() {
			t.Skip("production requires explicit ZEROPS_ACC_TOKEN_ADMIN=1 and pre-authorized ZEROPS_ACC_TOKEN_ADMIN_TOKEN; no ambient-credential capability probe is permitted")
		}
		if os.Getenv("ZEROPS_ACC_TOKEN_PROBE") != "1" {
			t.Skip("requires ZEROPS_ACC_TOKEN_ADMIN=1 and separate pre-authorized ZEROPS_ACC_TOKEN_ADMIN_TOKEN; optional ZEROPS_ACC_TOKEN_PROBE=1 attempts normal-bearer capability once; acceptance uses only pre-authorized credentials")
		}
		// One explicit capability attempt, never retry minting on ambiguous results.
		c := acctest.Client(t)
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		v, e := c.CreateIntegrationToken(ctx, org, client.IntegrationTokenWrite{Name: acctest.RandomName("tfacc-token-gate"), RoleCode: "NO_ACCESS", Projects: []client.IntegrationTokenProject{}})
		if v.ID != "" {
			if de := c.DeleteIntegrationToken(ctx, org, v.ID); de != nil {
				t.Fatalf("capability-probe token cleanup failed: %v", de)
			}
			if _, de := c.GetIntegrationToken(ctx, org, v.ID); !client.IsNotFound(de, "clientUserConnectionNotFound") {
				t.Fatalf("capability-probe token absence not confirmed: %v", de)
			}
		}
		if e != nil {
			if api, ok := e.(*client.APIError); ok && (api.Code == "notAllowedForApplicationToken" || api.Code == "notAllowedForIntegrationTokenWithoutDelegation" || api.Code == "insufficientPermissions") {
				t.Skipf("normal dev acceptance credential cannot administer tokens: HTTP %d %s; ZEROPS_ACC_TOKEN_ADMIN=1 and pre-authorized ZEROPS_ACC_TOKEN_ADMIN_TOKEN absent. Acceptance never automates login", api.HTTPStatus, api.Code)
			}
			t.Fatalf("token capability attempt failed (not a privilege skip): %v", e)
		}
		t.Skip("normal bearer capability probe succeeded and token was deleted, but explicit ZEROPS_ACC_TOKEN_ADMIN=1 and separate pre-authorized ZEROPS_ACC_TOKEN_ADMIN_TOKEN are required")
	}
	if !acctest.IsProd() && admin == cfg.Token {
		t.Fatal("administration credential must be separate from ordinary provider bearer")
	}
	project := acctest.CreateTestProject(t)
	t.Setenv("ZEROPS_TOKEN", admin)
	c := acctest.Client(t)
	name := acctest.RandomName("tfacc-token")
	var id, oldSecret string
	deleteResource := &Resource{data: &providerdata.ProviderData{API: c, Locks: &locks.Keyed{}}}
	var deleteSchema frameworkresource.SchemaResponse
	deleteResource.Schema(context.Background(), frameworkresource.SchemaRequest{}, &deleteSchema)
	var deleteState tfsdk.State
	// Registered after project cleanup, so token cleanup runs first even on failed assertions.
	t.Cleanup(func() {
		if id == "" {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		// Destroy may already have confirmed absence; the guard needs live metadata
		// to authorize deletion, so do not repeat a mutation after that evidence is gone.
		_, e := c.GetIntegrationToken(ctx, org, id)
		if client.IsNotFound(e, "clientUserConnectionNotFound") {
			return
		}
		if e != nil {
			t.Errorf("token cleanup read: %v", e)
			return
		}
		e = c.DeleteIntegrationToken(ctx, org, id)
		if e != nil && !client.IsNotFound(e, "clientUserConnectionNotFound") {
			t.Errorf("token cleanup: %v", e)
			return
		}
		if _, e = c.GetIntegrationToken(ctx, org, id); !client.IsNotFound(e, "clientUserConnectionNotFound") {
			t.Errorf("token absence: %v", e)
		}
	})
	config := func(suffix, generation string, sudo bool) string {
		projectRole := "READ_ONLY"
		if suffix != "" {
			projectRole = "BASIC_USER"
		}
		if acctest.IsProd() {
			sudo = false
		}
		return acctest.ProviderConfig() + fmt.Sprintf(`
resource "zerops_integration_token" "test" {
 name = %q
 role_code = "NO_ACCESS"
 projects = [{project_id = %q, role_code = %q}]
 sudo = %t
 can_create_projects = false
 can_view_finances = false
 can_edit_finances = false
 rotation_trigger = {generation = %q}
}
`, name+suffix, project.ID, projectRole, sudo, generation)
	}
	authStatus := func(secret string) (int, error) {
		req, e := http.NewRequest(http.MethodGet, strings.TrimRight(cfg.Endpoint, "/")+"/api/rest/public/user/info", nil)
		if e != nil {
			return 0, e
		}
		req.Header.Set("Authorization", "Bearer "+secret)
		hc := http.Client{Transport: cfg.Transport, Timeout: time.Minute}
		res, e := hc.Do(req)
		if e != nil {
			return 0, e
		}
		defer res.Body.Close()
		return res.StatusCode, nil
	}
	capture := func(s *terraform.State) error {
		v := s.RootModule().Resources["zerops_integration_token.test"]
		_, id, _ = strings.Cut(v.Primary.ID, "/")
		oldSecret = v.Primary.Attributes["token"]
		if oldSecret == "" {
			return fmt.Errorf("create secret absent")
		}
		status, e := authStatus(oldSecret)
		if e != nil {
			return e
		}
		if status != 200 {
			return fmt.Errorf("new token auth HTTP %d", status)
		}
		// Save framework state while this same token exists. The final direct
		// Delete exercises guarded absence after ordinary CLI destroy proves cleanup.
		imported := frameworkresource.ImportStateResponse{State: tfsdk.State{Schema: deleteSchema.Schema}}
		deleteResource.ImportState(context.Background(), frameworkresource.ImportStateRequest{ID: org + "/" + id}, &imported)
		if imported.Diagnostics.HasError() {
			return fmt.Errorf("saving token Delete state: %v", imported.Diagnostics)
		}
		deleteState = imported.State
		return nil
	}
	retained := func(s *terraform.State) error {
		if s.RootModule().Resources["zerops_integration_token.test"].Primary.Attributes["token"] != oldSecret {
			return fmt.Errorf("metadata update changed secret")
		}
		return nil
	}
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(provider.Registry{Resources: []func() frameworkresource.Resource{NewResource}}), CheckDestroy: func(_ *terraform.State) error {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		_, e := c.GetIntegrationToken(ctx, org, id)
		if !client.IsNotFound(e, "clientUserConnectionNotFound") {
			return fmt.Errorf("token not absent: %v", e)
		}
		return nil
	}, Steps: []resource.TestStep{
		{Config: config("", "1", false), Check: capture},
		{Config: config("-renamed", "1", true), Check: retained},
		{Config: config("-renamed", "1", true), PlanOnly: true},
		{ResourceName: "zerops_integration_token.test", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"token", "rotation_trigger"}},
		{Config: config("-renamed", "2", false), Check: func(s *terraform.State) error {
			secret := s.RootModule().Resources["zerops_integration_token.test"].Primary.Attributes["token"]
			if secret == "" || secret == oldSecret {
				return fmt.Errorf("rotation did not change secret")
			}
			status, e := authStatus(oldSecret)
			if e != nil {
				return e
			}
			if status != 401 {
				return fmt.Errorf("old token auth HTTP %d; expected immediate 401", status)
			}
			status, e = authStatus(secret)
			if e != nil {
				return e
			}
			if status != 200 {
				return fmt.Errorf("rotated token auth HTTP %d", status)
			}
			return nil
		}},
	}})
	if t.Failed() {
		t.Fatal("ordinary token lifecycle must pass before guarded absence exercise")
	}
	if deleteState.Schema == nil {
		t.Fatal("token lifecycle did not save framework Delete state")
	}
	deleted := frameworkresource.DeleteResponse{State: deleteState}
	deleteResource.Delete(context.Background(), frameworkresource.DeleteRequest{State: deleteState}, &deleted)
	if deleted.Diagnostics.HasError() {
		t.Fatalf("guarded framework Delete after ordinary CLI token cleanup: %v", deleted.Diagnostics)
	}
}
