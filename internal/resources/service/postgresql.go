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

// catalogService retains PostgreSQL identity lookup for the disk floor.
func catalogService(settings client.Settings, name string) (bool, *client.VerticalAutoscaling) {
	return catalogFamily(settings, name, "postgresql")
}

// catalogFamily resolves canonical catalogue identities using type ID/name,
// then version-name prefixes for older catalogues without distinct identities.
func catalogFamily(settings client.Settings, name, family string) (bool, *client.VerticalAutoscaling) {
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
			matches := strings.EqualFold(typ.ID, family) || strings.EqualFold(typ.Name, family) || strings.EqualFold(typ.Category, family)
			if !matches {
				matches = familyVersion(version.Name, family)
			}
			defaults := version.Config.VerticalAutoscaling
			if version.Config.DefaultAutoscaling != nil && version.Config.DefaultAutoscaling.Vertical != nil {
				defaults = version.Config.DefaultAutoscaling.Vertical
			}
			return matches, defaults
		}
		if typ.Name != "" && strings.EqualFold(base(typ.Name), nameBase) {
			return strings.EqualFold(typ.ID, family) || strings.EqualFold(typ.Name, family) || strings.EqualFold(typ.Category, family), nil
		}
	}
	return familyVersion(name, family), nil
}

func postgresqlVersion(name string) bool {
	return familyVersion(name, "postgresql")
}

func familyVersion(name, family string) bool {
	name = strings.ToLower(name)
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	return strings.HasPrefix(name, family+"@") || strings.HasPrefix(name, family+":")
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
	// Preserve framework-computed unknown defaults until service identity is known.
	if typ.IsUnknown() {
		return
	}
	// Other families retain their original optional scaling semantics.
	vertical := configured
	if !typ.IsNull() {
		settings, err := r.data.API.Settings(ctx, r.data.OrganizationID)
		if !checkServiceMode(typ.ValueString(), settings, err, resp) {
			return
		}
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
		for _, ram := range []struct {
			family, name, env string
			floor, fallback   float64
		}{
			{"clickhouse", "ClickHouse", "ZEROPS_CLICKHOUSE_MIN_RAM_GB", r.data.ClickhouseMinRAMGB, 0.375},
			{"elasticsearch", "Elasticsearch", "ZEROPS_ELASTICSEARCH_MIN_RAM_GB", r.data.ElasticsearchMinRAMGB, 2},
		} {
			matches, defaults := familyVersion(typ.ValueString(), ram.family), (*client.VerticalAutoscaling)(nil)
			if err != nil && matches {
				resp.Diagnostics.AddAttributeError(path.Root("type"), "Cannot resolve "+ram.name+" scaling defaults", "The service catalog is required to plan the "+ram.name+" RAM floor: "+err.Error())
				return
			}
			if err == nil {
				matches, defaults = catalogFamily(settings, typ.ValueString(), ram.family)
			}
			if matches {
				floor := ram.floor
				if floor == 0 {
					floor = ram.fallback
				}
				var maxRAM *float64
				if defaults != nil && defaults.MaxResource != nil {
					maxRAM = defaults.MaxResource.MemoryGBytes
				}
				vertical = managedScaling(configured, floor, "ram_gb", ram.name+" RAM below provider floor", fmt.Sprintf("The platform currently fails to provision %s with less than %g GB of RAM. The floor is set by environment variable %s.", ram.name, floor, ram.env), defaults != nil, maxRAM, &resp.Diagnostics)
			}
		}
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("vertical_autoscaling"), vertical)...)
}

func postgresqlScaling(value types.Object, floor float64, defaults *client.VerticalAutoscaling, diagnostics *diag.Diagnostics) types.Object {
	var maximum *float64
	if defaults != nil && defaults.MaxResource != nil {
		maximum = defaults.MaxResource.DiskGBytes
	}
	return managedScaling(value, floor, "disk_gb", "PostgreSQL disk below provider floor", fmt.Sprintf("The platform currently fails to provision PostgreSQL with less than %g GB of disk (generic HTTP 500 error). The floor is set by provider attribute postgresql_min_disk_gb / environment variable ZEROPS_POSTGRESQL_MIN_DISK_GB.", floor), defaults != nil, maximum, diagnostics)
}

func managedScaling(value types.Object, floor float64, resource, summary, detail string, hasDefaults bool, maximum *float64, diagnostics *diag.Diagnostics) types.Object {
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
	minKey, maxKey := "min_"+resource, "max_"+resource
	for _, key := range []string{minKey, maxKey} {
		number := attrs[key].(types.Number)
		if !number.IsNull() && !number.IsUnknown() && number.ValueBigFloat().Cmp(floorValue.ValueBigFloat()) < 0 {
			diagnostics.AddAttributeError(path.Root("vertical_autoscaling").AtName(key), summary, detail)
		}
	}
	if attrs[minKey].IsNull() {
		attrs[minKey] = floorValue
	}
	minimum := attrs[minKey].(types.Number)
	if attrs[maxKey].IsNull() && minimum.IsUnknown() && hasDefaults {
		attrs[maxKey] = types.NumberUnknown()
	}
	if attrs[maxKey].IsNull() && !minimum.IsUnknown() && maximum != nil {
		effectiveMin := floor
		if minimum.ValueBigFloat().Cmp(floorValue.ValueBigFloat()) > 0 {
			effectiveMin, _ = minimum.ValueBigFloat().Float64()
		}
		if *maximum < effectiveMin {
			attrs[maxKey] = nval(&effectiveMin, false)
		}
	}
	return types.ObjectValueMust(ScalingTypes(true), attrs)
}
