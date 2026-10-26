package project_env

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"strings"
)

type keyValidator struct{}

func (keyValidator) Description(context.Context) string {
	return "SYSTEM environment keys are reserved for zerops_project"
}
func (v keyValidator) MarkdownDescription(c context.Context) string { return v.Description(c) }
func (v keyValidator) ValidateString(c context.Context, r validator.StringRequest, p *validator.StringResponse) {
	if r.ConfigValue.IsNull() || r.ConfigValue.IsUnknown() {
		return
	}
	switch strings.ToLower(r.ConfigValue.ValueString()) {
	case "envisolation", "sshisolation", "apicdnurl", "staticcdnurl", "storagecdnurl", "zeropslocation", "zeropssubdomainstring", "zeropssubdomainhost":
		p.Diagnostics.AddAttributeError(r.Path, "Reserved SYSTEM key", v.Description(c))
	}
}

type contentValidator struct{}

func (contentValidator) Description(context.Context) string {
	return "must contain at most 8192 ASCII characters and no CR or LF"
}
func (v contentValidator) MarkdownDescription(c context.Context) string { return v.Description(c) }
func (v contentValidator) ValidateString(c context.Context, r validator.StringRequest, p *validator.StringResponse) {
	if r.ConfigValue.IsNull() || r.ConfigValue.IsUnknown() {
		return
	}
	s := r.ConfigValue.ValueString()
	invalid := len(s) > 8192
	for _, b := range []byte(s) {
		if b > 127 || b == '\r' || b == '\n' {
			invalid = true
			break
		}
	}
	if invalid {
		p.Diagnostics.AddAttributeError(r.Path, "Invalid environment content", v.Description(c))
	}
}
