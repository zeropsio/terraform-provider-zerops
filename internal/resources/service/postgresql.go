package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
)

// catalogService resolves canonical catalog identities rather than assuming the
// configured version starts with a particular database name. The catalog has no
// dedicated PostgreSQL enum, so its type ID/name are used; version-name prefixes
// are a fallback for older catalogs that omit a distinct type identity.
func catalogService(settings client.Settings, name string) (bool, *client.VerticalAutoscaling) {
	base := func(s string) string {
		if i := strings.LastIndex(s, "/"); i >= 0 {
			s = s[i+1:]
		}
		if i := strings.IndexAny(s, ":@"); i >= 0 {
			return s[:i]
		}
		return s
	}
	versionNumber := func(s string) string {
		if i := strings.LastIndex(s, "@"); i >= 0 {
			return s[i+1:]
		}
		return ""
	}
	nameBase, nameVersion := base(name), versionNumber(name)
	for _, typ := range settings.ServiceTypes {
		for _, version := range typ.Versions {
			if version.Name != name && (typ.Name == "" || !strings.EqualFold(base(typ.Name), nameBase) || versionNumber(version.Name) != nameVersion) {
				continue
			}
			pg := strings.EqualFold(typ.ID, "postgresql") || strings.EqualFold(typ.Name, "postgresql") || strings.EqualFold(typ.Category, "postgresql")
			if !pg {
				pg = postgresqlVersion(version.Name)
			}
			defaults := version.Config.VerticalAutoscaling
			if version.Config.DefaultAutoscaling != nil && version.Config.DefaultAutoscaling.Vertical != nil {
				defaults = version.Config.DefaultAutoscaling.Vertical
			}
			return pg, defaults
		}
		if typ.Name != "" && strings.EqualFold(base(typ.Name), nameBase) {
			return strings.EqualFold(typ.ID, "postgresql") || strings.EqualFold(typ.Name, "postgresql") || strings.EqualFold(typ.Category, "postgresql"), nil
		}
	}
	return postgresqlVersion(name), nil
}

func postgresqlVersion(name string) bool {
	name = strings.ToLower(name)
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	return strings.HasPrefix(name, "postgresql@") || strings.HasPrefix(name, "postgresql:")
}

func (r *Resource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if r.data == nil || req.Plan.Raw.IsNull() {
		return
	}
	var typ types.String
	var configured types.Object
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("type"), &typ)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("vertical_autoscaling"), &configured)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// The final service type may resolve to PostgreSQL during apply. Preserve
	// framework-computed unknown defaults until its identity is known.
	if typ.IsUnknown() {
		return
	}
	// Restore the original optional semantics for non-PostgreSQL services, even
	// though the shared schema permits PostgreSQL's provider-computed defaults.
	vertical := configured
	if !typ.IsNull() {
		settings, err := r.data.API.Settings(ctx, r.data.OrganizationID)
		// Other services retain their original plan behavior when catalog
		// discovery fails. PostgreSQL needs the catalog's default bounds to plan
		// a consistent disk range, so its canonical version spelling fails closed.
		pg, defaults := postgresqlVersion(typ.ValueString()), (*client.VerticalAutoscaling)(nil)
		if err != nil && pg {
			resp.Diagnostics.AddAttributeError(path.Root("type"), "Cannot resolve PostgreSQL scaling defaults", "The service catalog is required to plan the PostgreSQL disk floor: "+err.Error())
			return
		}
		if err == nil {
			pg, defaults = catalogService(settings, typ.ValueString())
		}
		if pg {
			floor := r.data.PostgresqlMinDiskGB
			if floor == 0 {
				floor = 2
			}
			vertical = postgresqlScaling(configured, floor, defaults, &resp.Diagnostics)
		}
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("vertical_autoscaling"), vertical)...)
}

func postgresqlScaling(value types.Object, floor float64, defaults *client.VerticalAutoscaling, diagnostics *diag.Diagnostics) types.Object {
	if value.IsUnknown() {
		return value
	}
	attrs := value.Attributes()
	if value.IsNull() {
		attrs = make(map[string]attr.Value, len(ScalingTypes(true)))
		for key, typ := range ScalingTypes(true) {
			switch typ {
			case types.NumberType:
				attrs[key] = types.NumberNull()
			case types.Int64Type:
				attrs[key] = types.Int64Null()
			case types.StringType:
				attrs[key] = types.StringNull()
			case types.BoolType:
				attrs[key] = types.BoolNull()
			}
		}
	}
	floorValue := nval(&floor, false)
	for _, key := range []string{"min_disk_gb", "max_disk_gb"} {
		number := attrs[key].(types.Number)
		if !number.IsNull() && !number.IsUnknown() && number.ValueBigFloat().Cmp(floorValue.ValueBigFloat()) < 0 {
			diagnostics.AddAttributeError(path.Root("vertical_autoscaling").AtName(key), "PostgreSQL disk below provider floor", fmt.Sprintf("The platform currently fails to provision PostgreSQL with less than %g GB of disk (generic HTTP 500 error). The floor is set by provider attribute postgresql_min_disk_gb / environment variable ZEROPS_POSTGRESQL_MIN_DISK_GB.", floor))
		}
	}
	if attrs["min_disk_gb"].IsNull() {
		attrs["min_disk_gb"] = floorValue
	}
	minimum := attrs["min_disk_gb"].(types.Number)
	if attrs["max_disk_gb"].IsNull() && minimum.IsUnknown() && defaults != nil {
		attrs["max_disk_gb"] = types.NumberUnknown()
	}
	if attrs["max_disk_gb"].IsNull() && !minimum.IsUnknown() && defaults != nil && defaults.MaxResource != nil && defaults.MaxResource.DiskGBytes != nil {
		effectiveMin := floor
		if minimum.ValueBigFloat().Cmp(floorValue.ValueBigFloat()) > 0 {
			effectiveMin, _ = minimum.ValueBigFloat().Float64()
		}
		if *defaults.MaxResource.DiskGBytes < effectiveMin {
			attrs["max_disk_gb"] = nval(&effectiveMin, false)
		}
	}
	return types.ObjectValueMust(ScalingTypes(true), attrs)
}
