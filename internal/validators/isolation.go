package validators

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"strings"
	"unicode"
)

// Isolation validates token structure without freezing the server's extensible vocabulary.
func Isolation(ssh bool) validator.String { return isolationValidator{ssh: ssh} }

type isolationValidator struct{ ssh bool }

func (isolationValidator) Description(context.Context) string {
	return "must contain nonempty isolation tokens, optionally negated, separated by commas"
}
func (v isolationValidator) MarkdownDescription(c context.Context) string { return v.Description(c) }
func (v isolationValidator) ValidateString(ctx context.Context, r validator.StringRequest, p *validator.StringResponse) {
	if r.ConfigValue.IsNull() || r.ConfigValue.IsUnknown() {
		return
	}
	for _, token := range strings.Split(r.ConfigValue.ValueString(), ",") {
		token = strings.TrimSpace(token)
		if strings.HasPrefix(token, "-") {
			if !v.ssh && !strings.HasPrefix(token, "-service@") {
				p.Diagnostics.AddAttributeError(r.Path, "Invalid isolation", v.Description(ctx))
				return
			}
			token = token[1:]
		}
		if token == "" || strings.ContainsAny(token, "/\\\r\n") || strings.IndexFunc(token, unicode.IsSpace) >= 0 || strings.HasSuffix(token, "@") {
			p.Diagnostics.AddAttributeError(r.Path, "Invalid isolation", v.Description(ctx))
			return
		}
	}
}
