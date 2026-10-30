package project_import

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
)

type modeAPI struct {
	client.API
	settings client.Settings
	err      error
}

func (a modeAPI) Settings(context.Context, string) (client.Settings, error) {
	return a.settings, a.err
}

func TestImportServiceModePlan(t *testing.T) {
	catalog := client.Settings{ServiceTypes: []client.ServiceType{{Versions: []client.ServiceTypeVersion{{Name: "postgresql:single@16"}, {Name: "postgresql:ha@16"}, {Name: "newdb:single@1"}, {Name: "newdb:ha@1"}, {Name: "meilisearch:single@1.44"}}}}}
	for _, tc := range []struct {
		name, definition                    string
		unknown, fallback, unavailable, bad bool
		options                             []string
	}{
		{name: "shorthand", definition: "type: postgresql@16", bad: true, options: []string{"postgresql:single@<version>", "postgresql:ha@<version>"}},
		{name: "single", definition: "type: postgresql:single@16"},
		{name: "ha", definition: "type: postgresql:ha@16"},
		{name: "mode-ha", definition: "type: postgresql@16, mode: HA"},
		{name: "mode-single", definition: "type: postgresql@16, mode: NON_HA"},
		{name: "unknown", unknown: true},
		{name: "runtime", definition: "type: alpine/nodejs@22"},
		{name: "single-only", definition: "type: meilisearch@1.44"},
		{name: "catalog-only", definition: "type: newdb@1", bad: true, options: []string{"newdb:single@<version>", "newdb:ha@<version>"}},
		{name: "no-provider", definition: "type: valkey@7.2", fallback: true, bad: true, options: []string{"valkey:single@<version>", "valkey:ha@<version>"}},
		{name: "unavailable", definition: "type: valkey@7.2", unavailable: true, bad: true, options: []string{"valkey:single@<version>", "valkey:ha@<version>"}},
		{name: "sensitive-version", definition: "type: PostgreSQL@private-version-token", bad: true, options: []string{"postgresql:single@<version>", "postgresql:ha@<version>"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			r := &Resource{}
			if !tc.fallback {
				api := modeAPI{settings: catalog}
				if tc.unavailable {
					api.err = errors.New("unavailable")
				}
				r.data = &providerdata.ProviderData{API: api, OrganizationID: "org"}
			}
			var sr resource.SchemaResponse
			r.Schema(ctx, resource.SchemaRequest{}, &sr)
			yaml := types.StringValue("project: {name: test}\nservices: [{hostname: secret-customer-hostname, " + tc.definition + "}]")
			if tc.unknown {
				yaml = types.StringUnknown()
			}
			m := Model{OrganizationID: types.StringValue("org"), YAML: yaml, Services: types.MapUnknown(types.StringType), Timeouts: timeouts.Value{Object: types.ObjectNull(map[string]attr.Type{"create": types.StringType, "update": types.StringType, "delete": types.StringType})}}
			plan := tfsdk.Plan{Schema: sr.Schema}
			if d := plan.Set(ctx, &m); d.HasError() {
				t.Fatal(d)
			}
			response := resource.ModifyPlanResponse{Plan: plan}
			r.ModifyPlan(ctx, resource.ModifyPlanRequest{Plan: plan, Config: tfsdk.Config{Schema: sr.Schema, Raw: plan.Raw}, State: tfsdk.State{Schema: sr.Schema}}, &response)
			if response.Diagnostics.HasError() != tc.bad {
				t.Fatal(response.Diagnostics)
			}
			if tc.bad && (len(response.Diagnostics) != 1 || !strings.Contains(response.Diagnostics[0].Detail(), "services[0].type")) {
				t.Fatal(response.Diagnostics)
			}
			for _, option := range tc.options {
				if !strings.Contains(response.Diagnostics[0].Detail(), option) {
					t.Fatalf("missing %s: %v", option, response.Diagnostics)
				}
			}
			for _, diagnostic := range response.Diagnostics {
				for _, secret := range []string{"secret-customer-hostname", "private-version-token", "PostgreSQL@"} {
					if strings.Contains(diagnostic.Summary()+diagnostic.Detail(), secret) {
						t.Fatalf("diagnostic exposes sensitive recipe content: %s", secret)
					}
				}
			}
		})
	}
}
