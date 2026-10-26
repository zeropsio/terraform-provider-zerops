package public_http_routing

import (
	"context"
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

var identifier = regexp.MustCompile(`^[a-zA-Z]+[a-zA-Z0-9_]*$`)

type configStringValidator struct {
	description string
	valid       func(string) bool
}

func (v configStringValidator) Description(context.Context) string { return v.description }
func (v configStringValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}
func (v configStringValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if !v.valid(req.ConfigValue.ValueString()) {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid location configuration", v.description)
	}
}

type configObjectValidator struct{}

func (configObjectValidator) Description(context.Context) string {
	return "A config must contain at least one non-null child."
}
func (v configObjectValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}
func (v configObjectValidator) ValidateObject(_ context.Context, req validator.ObjectRequest, resp *validator.ObjectResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	for _, child := range req.ConfigValue.Attributes() {
		if child.IsUnknown() || !child.IsNull() {
			return
		}
	}
	resp.Diagnostics.AddAttributeError(req.Path, "Empty location config", v.Description(context.Background()))
}

func locationConfigSchema() schema.SingleNestedAttribute {
	enabled := func() schema.BoolAttribute {
		return schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false)}
	}
	text := func(value string, validators ...validator.String) schema.StringAttribute {
		return schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString(value), Validators: validators}
	}
	number := func(value, min, max int64) schema.Int64Attribute {
		return schema.Int64Attribute{Optional: true, Computed: true, Default: int64default.StaticInt64(value), Validators: []validator.Int64{int64validator.Between(min, max)}}
	}
	ident := stringvalidator.RegexMatches(identifier, "must be an identifier starting with a letter")
	cidr := configStringValidator{"must be an IPv4 or IPv6 CIDR", func(s string) bool { _, err := netip.ParsePrefix(s); return err == nil }}
	cidrs := func(required bool) schema.ListAttribute {
		return schema.ListAttribute{Required: required, Optional: !required, ElementType: types.StringType, Validators: []validator.List{listvalidator.ValueStringsAre(cidr)}}
	}
	child := func(fields map[string]schema.Attribute) schema.SingleNestedAttribute {
		fields["enabled"] = enabled()
		return schema.SingleNestedAttribute{Optional: true, Attributes: fields}
	}
	return schema.SingleNestedAttribute{Optional: true, MarkdownDescription: "Optional location controls. At least one child is required. Basic-auth passwords are stored in Terraform state; protect state access.", Validators: []validator.Object{configObjectValidator{}}, Attributes: map[string]schema.Attribute{
		"redirect": child(map[string]schema.Attribute{
			"to": schema.StringAttribute{Required: true, Validators: []validator.String{configStringValidator{"must be a URL or absolute path without credentials or fragment", func(s string) bool {
				u, e := url.Parse(s)
				return e == nil && s != "" && u.User == nil && u.Fragment == "" && u.Opaque == "" && (strings.HasPrefix(s, "/") || (u.Host != "" && (u.Scheme == "https" || u.Scheme == "http")))
			}}}},
			"code": number(302, 100, 600), "preserve_path": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false)}, "preserve_query": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false)},
		}),
		"basic_auth": child(map[string]schema.Attribute{
			"realm": schema.StringAttribute{Required: true, Validators: []validator.String{ident}},
			"users": schema.ListNestedAttribute{Required: true, NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
				"username": schema.StringAttribute{Required: true, Validators: []validator.String{ident, stringvalidator.LengthAtMost(32)}},
				"password": schema.StringAttribute{Required: true, Sensitive: true, Validators: []validator.String{configStringValidator{"password must be at most 72 bytes", func(s string) bool { return len(s) <= 72 }}}},
				"comment":  schema.StringAttribute{Required: true},
			}}},
		}),
		"access_policy": child(map[string]schema.Attribute{
			"cidr": cidrs(true), "default_policy": text("allow", stringvalidator.OneOf("allow", "deny")),
			"real_ip_header": text("X-Forwarded-For", stringvalidator.RegexMatches(regexp.MustCompile("^[!#$%&'*+.^_`|~0-9a-zA-Z-]+$"), "must be an HTTP header token")), "set_real_ip_from": cidrs(false),
		}),
		"content": child(map[string]schema.Attribute{
			"code": number(200, 100, 600), "content": text("Hello world!", configStringValidator{"content must be at most 50 KiB", func(s string) bool { return len(s) <= 50*1024 }}), "content_type": text("application/octet-stream", stringvalidator.RegexMatches(regexp.MustCompile(`^[a-zA-Z0-9_-]+/[a-zA-Z0-9_-]+$`), "must be a media type")),
		}),
		"rate_limiting": child(map[string]schema.Attribute{
			"key": text("binary_remote_addr", stringvalidator.OneOf("binary_remote_addr", "server_name")), "zone": text("mainzone", ident), "rate": number(10, 1, 9223372036854775807), "burst": number(10, 1, 9223372036854775807), "zone_size": number(10, 1, 50),
		}),
	}}
}

// The API uses camelCase while Terraform uses snake_case. Keep the translation
// at this boundary and preserve ordered lists and explicit null children.
func configWireName(name string) string {
	parts := strings.Split(name, "_")
	for i := 1; i < len(parts); i++ {
		parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
	}
	return strings.Join(parts, "")
}
func configWire(v attr.Value) (any, error) {
	if v.IsUnknown() {
		return nil, fmt.Errorf("location configuration remains unknown")
	}
	if v.IsNull() {
		return nil, nil
	}
	switch v := v.(type) {
	case types.Object:
		out := map[string]any{}
		for k, a := range v.Attributes() {
			value, e := configWire(a)
			if e != nil {
				return nil, e
			}
			out[configWireName(k)] = value
		}
		return out, nil
	case types.List:
		out := make([]any, 0, len(v.Elements()))
		for _, a := range v.Elements() {
			value, e := configWire(a)
			if e != nil {
				return nil, e
			}
			out = append(out, value)
		}
		return out, nil
	case types.String:
		return v.ValueString(), nil
	case types.Bool:
		return v.ValueBool(), nil
	case types.Int64:
		return v.ValueInt64(), nil
	default:
		return nil, fmt.Errorf("unsupported location configuration value")
	}
}
func configValue(ctx context.Context, typ attr.Type, raw any) (attr.Value, error) {
	if m, ok := raw.(map[string]any); ok && m == nil {
		raw = nil
	}
	if raw == nil {
		v, e := typ.ValueFromTerraform(ctx, tftypes.NewValue(typ.TerraformType(ctx), nil))
		return v, e
	}
	switch typ := typ.(type) {
	case types.ObjectType:
		m, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid location config object")
		}
		vals := map[string]attr.Value{}
		for k, t := range typ.AttrTypes {
			v, e := configValue(ctx, t, m[configWireName(k)])
			if e != nil {
				return nil, e
			}
			vals[k] = v
		}
		v, d := types.ObjectValue(typ.AttrTypes, vals)
		if d.HasError() {
			return nil, fmt.Errorf("invalid location config object")
		}
		return v, nil
	case types.ListType:
		list, ok := raw.([]any)
		if !ok {
			return nil, fmt.Errorf("invalid location config list")
		}
		vals := make([]attr.Value, len(list))
		for i, r := range list {
			v, e := configValue(ctx, typ.ElemType, r)
			if e != nil {
				return nil, e
			}
			vals[i] = v
		}
		v, d := types.ListValue(typ.ElemType, vals)
		if d.HasError() {
			return nil, fmt.Errorf("invalid location config list")
		}
		return v, nil
	case basetypes.StringType:
		if v, ok := raw.(string); ok {
			return types.StringValue(v), nil
		}
	case basetypes.BoolType:
		if v, ok := raw.(bool); ok {
			return types.BoolValue(v), nil
		}
	case basetypes.Int64Type:
		switch v := raw.(type) {
		case float64:
			return types.Int64Value(int64(v)), nil
		case int64:
			return types.Int64Value(v), nil
		case int:
			return types.Int64Value(int64(v)), nil
		}
	}
	return nil, fmt.Errorf("invalid location config field type")
}

func normalizedConfig(m map[string]any) map[string]any {
	for _, v := range m {
		if v != nil {
			return m
		}
	}
	return nil
}
