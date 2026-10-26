package project_env

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
	"strings"
	"testing"
)

type maskedAPI struct {
	client.API
	row       client.Env
	revealErr error
	value     string
}

func (a maskedAPI) GetProjectEnv(context.Context, string) (client.Env, error) { return a.row, nil }
func (a maskedAPI) RevealProjectEnv(context.Context, string) (string, error) {
	return a.value, a.revealErr
}
func TestReadMasked(t *testing.T) {
	for _, tc := range []struct {
		name          string
		prior         types.String
		err           error
		wantErr, warn bool
	}{{"known non sudo", types.StringValue("private-value"), &client.APIError{HTTPStatus: 403, Code: "notInSudoMode"}, false, true}, {"import non sudo", types.StringNull(), &client.APIError{HTTPStatus: 403, Code: "notInSudoMode"}, true, false}, {"other error", types.StringValue("private-value"), &client.APIError{HTTPStatus: 403, Code: "insufficientPermissions"}, true, false}, {"sudo", types.StringNull(), nil, false, false}} {
		t.Run(tc.name, func(t *testing.T) {
			a := maskedAPI{row: client.Env{ID: "e", ProjectID: "p", Type: "USER", Key: "KEY", Sensitive: true, Content: "REDACTED"}, revealErr: tc.err, value: "private-value"}
			r := Resource{data: &providerdata.ProviderData{API: a}}
			m := Model{ID: types.StringValue("e"), Content: tc.prior}
			var d diag.Diagnostics
			e := r.read(context.Background(), &m, &d)
			if (e != nil) != tc.wantErr || d.WarningsCount() > 0 != tc.warn {
				t.Fatalf("err %v diagnostics %v", e, d)
			}
			if e == nil && m.Content.ValueString() != "private-value" {
				t.Fatal("masked content replaced known value")
			}
			if e != nil && strings.Contains(e.Error(), "private-value") {
				t.Fatal("secret leaked")
			}
		})
	}
}
