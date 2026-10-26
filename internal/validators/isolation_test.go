package validators

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"testing"
)

func TestIsolation(t *testing.T) {
	for _, tc := range []struct {
		s        string
		ssh, bad bool
	}{{"none", false, false}, {"service", false, false}, {"service@app,-service@db", false, false}, {"project,vpn,-service@app", true, false}, {"future_extension", true, false}, {"", false, true}, {"none,,service", false, true}, {"service@", false, true}, {"-project", false, true}, {"-vpn", true, false}, {"service\napp", true, true}} {
		p := validator.StringResponse{}
		Isolation(tc.ssh).ValidateString(context.Background(), validator.StringRequest{ConfigValue: types.StringValue(tc.s)}, &p)
		if p.Diagnostics.HasError() != tc.bad {
			t.Fatalf("%q ssh=%v diagnostics=%v", tc.s, tc.ssh, p.Diagnostics)
		}
	}
}
