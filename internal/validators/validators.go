// Package validators contains reusable Zerops schema constraints.
package validators

import (
	"context"
	"math/big"
	"net/netip"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	hostnameRE    = regexp.MustCompile(`^[a-z][a-z0-9]{0,39}$`)
	envKeyRE      = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
	serviceTypeRE = regexp.MustCompile(`^([a-z]+/)?[a-z-]+(:[a-z]+)?(@[a-z0-9.]+)?$`)
	decimalRE     = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`)
	domainLabelRE = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?$`)
	commitRE      = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)
)

func Hostname() validator.String {
	return stringvalidator.All(stringvalidator.RegexMatches(hostnameRE, "must be 1–40 lowercase letters/digits, starting with a letter"), stringvalidator.NoneOf("core", "l7httpbalancer"))
}
func EnvKey() validator.String {
	return stringCheck{"must be an environment key without a reserved ZEROPS_ or CORE_ZEROPS_ prefix", func(s string) bool {
		u := strings.ToUpper(s)
		return envKeyRE.MatchString(s) && !strings.HasPrefix(u, "ZEROPS_") && !strings.HasPrefix(u, "CORE_ZEROPS_")
	}}
}
func ServiceType() validator.String {
	return stringvalidator.RegexMatches(serviceTypeRE, "must be a service type with optional OS, mode and version")
}
func PositiveDecimalString() validator.String {
	return stringCheck{"must be a positive decimal string", func(s string) bool {
		if !decimalRE.MatchString(s) {
			return false
		}
		n, ok := new(big.Rat).SetString(s)
		return ok && n.Sign() > 0
	}}
}
func CIDROrIP() validator.String {
	return stringCheck{"must be an IPv4/IPv6 address or CIDR", func(s string) bool {
		_, e := netip.ParseAddr(s)
		if e == nil {
			return true
		}
		_, e = netip.ParsePrefix(s)
		return e == nil
	}}
}
func Domain() validator.String {
	return stringCheck{"must be a DNS domain, optionally prefixed with *.", validDomain}
}
func validDomain(s string) bool {
	s = strings.TrimPrefix(s, "*.")
	if len(s) > 253 || !strings.Contains(s, ".") {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if len(label) == 0 || len(label) > 63 || !domainLabelRE.MatchString(label) {
			return false
		}
	}
	return true
}
func GitBranch() validator.String {
	return stringCheck{"must be a Git branch name (not a ref, tag or commit ID)", func(s string) bool {
		if s == "" || s == "@" || strings.HasPrefix(s, "-") || strings.HasPrefix(s, "refs/") || strings.HasSuffix(s, ".") || strings.Contains(s, "..") || strings.Contains(s, "@{") || strings.ContainsAny(s, " ~^:?*[\\") || commitRE.MatchString(s) {
			return false
		}
		for _, r := range s {
			if r < 32 || r == 127 {
				return false
			}
		}
		for _, p := range strings.Split(s, "/") {
			if p == "" || strings.HasPrefix(p, ".") || strings.HasSuffix(p, ".lock") {
				return false
			}
		}
		return true
	}}
}
func Port() validator.Int64       { return int64validator.Between(1, 65535) }
func PublicPort() validator.Int64 { return publicPort{} }

type stringCheck struct {
	description string
	valid       func(string) bool
}

func (v stringCheck) Description(context.Context) string             { return v.description }
func (v stringCheck) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }
func (v stringCheck) ValidateString(ctx context.Context, r validator.StringRequest, out *validator.StringResponse) {
	if r.ConfigValue.IsNull() || r.ConfigValue.IsUnknown() {
		return
	}
	if !v.valid(r.ConfigValue.ValueString()) {
		out.Diagnostics.AddAttributeError(r.Path, "Invalid value", v.Description(ctx))
	}
}

type publicPort struct{}

func (publicPort) Description(context.Context) string {
	return "must be 10–65435 excluding reserved ports 80 and 443"
}
func (v publicPort) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }
func (v publicPort) ValidateInt64(ctx context.Context, r validator.Int64Request, out *validator.Int64Response) {
	if r.ConfigValue.IsNull() || r.ConfigValue.IsUnknown() {
		return
	}
	n := r.ConfigValue.ValueInt64()
	if n < 10 || n > 65435 || n == 80 || n == 443 {
		out.Diagnostics.AddAttributeError(r.Path, "Invalid public port", v.Description(ctx))
	}
}

// Precision limits configured numbers without rounding server readback values.
func Precision() validator.Number { return precision{} }

type precision struct{}

func (precision) Description(context.Context) string               { return "must have at most three decimal places" }
func (v precision) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }
func (v precision) ValidateNumber(ctx context.Context, r validator.NumberRequest, out *validator.NumberResponse) {
	if r.ConfigValue.IsNull() || r.ConfigValue.IsUnknown() {
		return
	}
	text := r.ConfigValue.ValueBigFloat().Text('f', -1)
	parts := strings.SplitN(text, ".", 2)
	if len(parts) == 2 && len(strings.TrimRight(parts[1], "0")) > 3 {
		out.Diagnostics.AddAttributeError(r.Path, "Invalid precision", v.Description(ctx))
	}
}

// MinMax compares named numeric leaves of a single nested object; unknown leaves defer validation.
func MinMax(min, max string) validator.Object { return minMax{min, max} }

type minMax struct{ min, max string }

func (v minMax) Description(context.Context) string {
	return v.min + " must be less than or equal to " + v.max
}
func (v minMax) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }
func (v minMax) ValidateObject(ctx context.Context, r validator.ObjectRequest, out *validator.ObjectResponse) {
	if r.ConfigValue.IsNull() || r.ConfigValue.IsUnknown() {
		return
	}
	a := r.ConfigValue.Attributes()
	lo, hi := a[v.min], a[v.max]
	if lo == nil || hi == nil || lo.IsNull() || hi.IsNull() || lo.IsUnknown() || hi.IsUnknown() {
		return
	}
	invalid := false
	switch l := lo.(type) {
	case types.Number:
		if h, ok := hi.(types.Number); ok {
			invalid = l.ValueBigFloat().Cmp(h.ValueBigFloat()) > 0
		}
	case types.Int64:
		if h, ok := hi.(types.Int64); ok {
			invalid = l.ValueInt64() > h.ValueInt64()
		}
	}
	if invalid {
		out.Diagnostics.AddAttributeError(r.Path, "Invalid bounds", v.Description(ctx))
	}
}
func NonEmptyObject() validator.Object { return nonEmptyObject{} }

type nonEmptyObject struct{}

func (nonEmptyObject) Description(context.Context) string {
	return "must configure at least one non-null attribute"
}
func (v nonEmptyObject) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }
func (v nonEmptyObject) ValidateObject(ctx context.Context, r validator.ObjectRequest, out *validator.ObjectResponse) {
	if r.ConfigValue.IsNull() || r.ConfigValue.IsUnknown() {
		return
	}
	for _, a := range r.ConfigValue.Attributes() {
		if !a.IsNull() {
			return
		}
	}
	out.Diagnostics.AddAttributeError(r.Path, "Empty autoscaling block", v.Description(ctx))
}
