package project_import

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/zeropsio/terraform-provider-zerops/internal/acctest"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/provider"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
	"github.com/zeropsio/terraform-provider-zerops/internal/waiter"
)

func TestRecipeValidation(t *testing.T) {
	for _, text := range []string{"", "project: [secret", "project: {name: x}", "project: {name: ''}\nservices: []", "project: {name: x}\nservices: []\n---\nsecret: value"} {
		if _, err := parseRecipe(text); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("unsafe validation for input")
		}
	}
	if _, err := parseRecipe("project: {name: arbitrary-user-name}\nservices: []"); err != nil {
		t.Fatal(err)
	}
}
func TestExpectedStatus(t *testing.T) {
	runtime := client.Service{ServiceTypeCategory: "RUNTIME"}
	if expectedStatus(runtime, nil) != "READY_TO_DEPLOY" {
		t.Fatal("undeployed runtime")
	}
	if expectedStatus(runtime, map[string]any{"buildFromGit": map[string]any{}}) != "ACTIVE" {
		t.Fatal("build requested")
	}
	if expectedStatus(runtime, map[string]any{"startWithoutCode": true}) != "ACTIVE" {
		t.Fatal("runtime requested to start without code")
	}
	if expectedStatus(client.Service{ServiceTypeCategory: "USER"}, nil) != "READY_TO_DEPLOY" {
		t.Fatal("undeployed USER runtime")
	}
	if expectedStatus(client.Service{ServiceTypeCategory: "MANAGED"}, nil) != "ACTIVE" {
		t.Fatal("managed")
	}
}

type readAPI struct {
	client.API
	rows    []client.Service
	offsets []int
}

func (a *readAPI) GetProject(context.Context, string) (client.Project, error) {
	return client.Project{ID: "p", Name: "actual", Status: "ACTIVE"}, nil
}
func (a *readAPI) ListServices(_ context.Context, _ string, q client.ServiceQuery) (client.Page[client.Service], error) {
	a.offsets = append(a.offsets, q.Offset)
	if q.Offset == 0 {
		return client.Page[client.Service]{Items: a.rows, Total: len(a.rows)}, nil
	}
	return client.Page[client.Service]{}, nil
}
func TestReadRetainsExactRecipeAndReflectsMissingChildren(t *testing.T) {
	api := &readAPI{rows: []client.Service{{ID: "core", Name: "core", IsSystem: true}, {ID: "s", Name: "added"}}}
	r := Resource{data: &providerdata.ProviderData{API: api}}
	original := types.StringValue("project: {name: secret}\nservices: []\n")
	m := Model{ID: types.StringValue("p"), YAML: original}
	if err := r.read(context.Background(), &m); err != nil {
		t.Fatal(err)
	}
	if !m.YAML.Equal(original) || len(m.Services.Elements()) != 1 || m.ProjectName.ValueString() != "actual" {
		t.Fatal("incorrect refresh")
	}
	api.rows = nil
	if err := r.read(context.Background(), &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Services.Elements()) != 0 {
		t.Fatal("missing child retained")
	}
	var d diag.Diagnostics
	safeError(&d, "operation", fmt.Errorf("secret recipe"))
	if strings.Contains(d[0].Detail(), "secret recipe") {
		t.Fatal("secret echoed")
	}
	if _, ok := NewResource().(fwresource.ResourceWithImportState); ok {
		t.Fatal("unsupported import exposed")
	}
}
func TestAccProjectImportLifecycle(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC not set")
	}
	acctest.PreCheck(t)
	_, organizationID, err := acctest.Environment()
	if err != nil {
		t.Fatal(err)
	}
	settings, err := acctest.Client(t).Settings(context.Background(), organizationID)
	if err != nil {
		t.Fatal(err)
	}
	if len(settings.Locations) == 0 {
		t.Fatal("organization has no available location")
	}
	location := settings.Locations[0].ID
	name := acctest.RandomName("tfacc")
	var oldID string
	ids := map[string]bool{}
	config := func(name string) string {
		limits := ""
		if acctest.IsProd() {
			limits = "    verticalAutoscaling:\n      cpu: 1\n      ram: 0.125\n      disk: 1\n      cpuMode: SHARED\n"
		}
		return acctest.ProviderConfig() + fmt.Sprintf(`resource "zerops_project_import" "test" {
 yaml = <<-YAML
project:
  name: %s
  corePackage: LIGHT
  location: %q
services:
  - hostname: cache
    type: valkey:single@7.2
%sYAML
}
`, name, location, limits)
	}
	check := func(replaced bool) resource.TestCheckFunc {
		return func(s *terraform.State) error {
			row := s.RootModule().Resources["zerops_project_import.test"].Primary
			if replaced && row.ID == oldID {
				return fmt.Errorf("recipe change did not replace project")
			}
			oldID = row.ID
			ids[row.ID] = true
			api := acctest.Client(t)
			p, err := api.GetProject(context.Background(), row.ID)
			if err != nil {
				return err
			}
			if p.Status != "ACTIVE" {
				return fmt.Errorf("project not ACTIVE")
			}
			service, err := api.GetService(context.Background(), row.Attributes["services.cache"])
			if err != nil {
				return err
			}
			if service.Status != "ACTIVE" {
				return fmt.Errorf("service not ACTIVE")
			}
			return nil
		}
	}
	var addedID string
	addChild := func(s *terraform.State) error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		api := acctest.Client(t)
		created, err := api.CreateService(ctx, oldID, acctest.MinimalServiceInput(client.ServiceCreate{Name: "added", ServiceStackVersionName: "valkey:single@7.2", UserData: []client.EnvWrite{}}))
		if err != nil {
			return err
		}
		addedID = created.Service.ID
		_, err = waiter.WaitProcess(ctx, api, created.Process.ID)
		if err == nil {
			_, err = waiter.WaitServiceSettled(ctx, api, addedID, []string{"ACTIVE"}, waiter.ProcessIDs{}, []string{created.Process.ID})
		}
		return err
	}
	removeChild := func(s *terraform.State) error {
		if s.RootModule().Resources["zerops_project_import.test"].Primary.Attributes["services.added"] != addedID {
			return fmt.Errorf("external child not observed")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		api := acctest.Client(t)
		process, err := api.DeleteService(ctx, addedID)
		if err == nil {
			_, err = waiter.WaitProcess(ctx, api, process.ID)
		}
		if err == nil {
			err = waiter.WaitNotFound(ctx, addedID, func(ctx context.Context) error { _, e := api.GetService(ctx, addedID); return e }, []string{"serviceStackNotFound"})
		}
		return err
	}
	resource.Test(t, resource.TestCase{PreCheck: func() { acctest.PreCheck(t) }, ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(provider.Registry{Resources: []func() fwresource.Resource{NewResource}}), CheckDestroy: func(_ *terraform.State) error {
		api := acctest.Client(t)
		for id := range ids {
			_, err := api.GetProject(context.Background(), id)
			if !client.IsNotFound(err, "projectNotFound") {
				return fmt.Errorf("project remains: %s", id)
			}
		}
		return nil
	}, Steps: []resource.TestStep{
		{Config: config(name), Check: resource.ComposeTestCheckFunc(check(false), addChild)},
		{Config: config(name), Check: removeChild},
		{Config: config(name), PlanOnly: true},
		{Config: config(name + "-replacement"), Check: check(true)},
		{Config: config(name + "-replacement"), PlanOnly: true},
	}})
}
