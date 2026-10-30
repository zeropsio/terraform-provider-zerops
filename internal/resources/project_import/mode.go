package project_import

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/validators"
)

func (r *Resource) checkServiceModes(ctx context.Context, m Model, diagnostics *diag.Diagnostics) {
	if m.YAML.IsNull() || m.YAML.IsUnknown() {
		return
	}
	recipe, err := parseRecipe(m.YAML.ValueString())
	if err != nil {
		diagnostics.AddAttributeError(path.Root("yaml"), "Invalid project recipe", err.Error())
		return
	}
	if len(recipe.Services) == 0 {
		return
	}
	// Without configured provider data (or a readable catalogue), use the
	// shared production/dev snapshot rather than skipping plan-time checks.
	var catalog *client.Settings
	if r.data != nil && r.data.API != nil && !m.OrganizationID.IsUnknown() {
		org := m.OrganizationID.ValueString()
		if m.OrganizationID.IsNull() {
			org = r.data.OrganizationID
		}
		if settings, err := r.data.API.Settings(ctx, org); err == nil {
			catalog = &settings
		}
	}
	families := validators.DualModeFamilies(catalog)
	for index, service := range recipe.Services {
		mode, _ := service["mode"].(string)
		if strings.EqualFold(mode, "HA") || strings.EqualFold(mode, "NON_HA") {
			continue
		}
		typ, _ := service["type"].(string)
		if family := validators.AmbiguousServiceFamily(typ, families); family != "" {
			// The recipe is sensitive: identify its entry without echoing any
			// hostname, configured spelling, or version from the YAML.
			diagnostics.AddAttributeError(path.Root("yaml"), "Ambiguous managed service type", fmt.Sprintf("services[%d].type uses an unqualified %q type, which Zerops resolves to a highly available (HA) service. Use %q for single-node (NON_HA) or %q for HA, or set mode.", index, family, family+":single@<version>", family+":ha@<version>"))
		}
	}
}
