package project_prometheus_config

import (
	"context"
	"net"
	"net/netip"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type targetsValidator struct{}

func (targetsValidator) Description(context.Context) string {
	return "Targets must be hostnames or IP addresses with optional valid ports."
}
func (v targetsValidator) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }
func (targetsValidator) ValidateList(ctx context.Context, req validator.ListRequest, resp *validator.ListResponse) {
	if req.ConfigValue.IsUnknown() || req.ConfigValue.IsNull() {
		return
	}
	var targets []types.String
	resp.Diagnostics.Append(req.ConfigValue.ElementsAs(ctx, &targets, false)...)
	for i, t := range targets {
		if !t.IsUnknown() && (t.IsNull() || !validTarget(t.ValueString())) {
			resp.Diagnostics.AddAttributeError(req.Path.AtListIndex(i), "Invalid Prometheus target", "Use a hostname or IP address with optional port 1–65535, without scheme, path or whitespace. Bracket IPv6 when specifying a port.")
		}
	}
}
func validTarget(s string) bool {
	if s == "" || strings.ContainsAny(s, "/\\?#@ \t\r\n") {
		return false
	}
	if _, e := netip.ParseAddr(s); e == nil {
		return true
	}
	host := s
	if strings.Contains(s, ":") || strings.HasPrefix(s, "[") {
		var port string
		var e error
		host, port, e = net.SplitHostPort(s)
		if e != nil {
			return false
		}
		n, e := strconv.Atoi(port)
		if e != nil || n < 1 || n > 65535 {
			return false
		}
		for _, c := range port {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	if _, e := netip.ParseAddr(host); e == nil {
		return true
	}
	if len(host) > 253 {
		return false
	}
	host = strings.TrimSuffix(host, ".")
	if host == "" {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}
