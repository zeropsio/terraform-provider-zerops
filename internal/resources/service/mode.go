package service

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/validators"
)

func checkServiceMode(name string, settings client.Settings, catalogErr error, resp *resource.ModifyPlanResponse) bool {
	catalog := &settings
	if catalogErr != nil {
		catalog = nil
	}
	if detail := validators.AmbiguousServiceMode(name, validators.DualModeFamilies(catalog)); detail != "" {
		resp.Diagnostics.AddAttributeError(path.Root("type"), "Ambiguous managed service type", detail)
		return false
	}
	return true
}

// canonicalTypeMigration permits only spelling the qualified type already in
// state, never changing its availability mode, family, or version.
func canonicalTypeMigration(old, planned, base types.String) bool {
	if old.IsNull() || old.IsUnknown() || planned.IsNull() || planned.IsUnknown() || base.IsNull() || base.IsUnknown() {
		return false
	}
	prior, next := old.ValueString(), planned.ValueString()
	if strings.ContainsAny(prior, "/:") || strings.Contains(next, "/") || !strings.EqualFold(next, base.ValueString()) {
		return false
	}
	family, version, ok := strings.Cut(prior, "@")
	qualified, nextVersion, nextOK := strings.Cut(next, "@")
	nextFamily, mode, qualifiedOK := strings.Cut(qualified, ":")
	return ok && nextOK && qualifiedOK && (strings.EqualFold(mode, "single") || strings.EqualFold(mode, "ha")) && strings.EqualFold(family, nextFamily) && strings.EqualFold(version, nextVersion)
}

func serviceTypeRequiresReplace() planmodifier.String {
	const description = "Changes replace the service unless qualifying a legacy shorthand to its existing canonical base."
	return stringplanmodifier.RequiresReplaceIf(func(ctx context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
		var base types.String
		resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("base"), &base)...)
		resp.RequiresReplace = !canonicalTypeMigration(req.StateValue, req.PlanValue, base)
	}, description, description)
}
