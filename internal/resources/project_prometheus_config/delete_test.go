package project_prometheus_config

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/locks"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
)

type deleteAPI struct {
	fakeAPI
	getErr, writeErr, finalErr error
	calls                      []string
}

func (a *deleteAPI) GetProject(_ context.Context, id string) (client.Project, error) {
	a.calls = append(a.calls, "get:"+id)
	if len(a.calls) == 1 {
		return client.Project{ID: id}, a.getErr
	}
	return client.Project{ID: id}, a.finalErr
}
func (a *deleteAPI) SetPrometheusConfig(ctx context.Context, id string, configs []client.PrometheusConfig) (*client.Process, error) {
	a.calls = append(a.calls, "reset:"+id)
	if a.writeErr != nil {
		return nil, a.writeErr
	}
	return a.fakeAPI.SetPrometheusConfig(ctx, id, configs)
}

func TestDeleteParentPreflight(t *testing.T) {
	absent := &client.APIError{HTTPStatus: 400, Code: "projectNotFound"}
	denied := &client.APIError{HTTPStatus: 403, Code: "forbidden"}
	for _, tc := range []struct {
		name                       string
		getErr, writeErr, finalErr error
		bad                        bool
		calls                      []string
	}{
		{"absent parent", absent, nil, nil, false, []string{"get:p"}},
		{"unexpected read error", errors.New("read failed"), nil, nil, true, []string{"get:p"}},
		{"transport error", &client.TransportError{}, nil, nil, true, []string{"get:p"}},
		{"permission error", denied, nil, nil, true, []string{"get:p"}},
		{"wrong status", &client.APIError{HTTPStatus: 403, Code: "projectNotFound"}, nil, nil, true, []string{"get:p"}},
		{"generic not found", &client.APIError{HTTPStatus: 404, Code: "notFound"}, nil, nil, true, []string{"get:p"}},
		{"normal reset", nil, nil, nil, false, []string{"get:p", "reset:p", "get:p"}},
		{"reset absence race", nil, absent, nil, false, []string{"get:p", "reset:p"}},
		{"reset error", nil, denied, nil, true, []string{"get:p", "reset:p"}},
		{"wait absence race", nil, nil, absent, false, []string{"get:p", "reset:p", "get:p"}},
		{"wait read error", nil, nil, denied, true, []string{"get:p", "reset:p", "get:p"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			api := &deleteAPI{getErr: tc.getErr, writeErr: tc.writeErr, finalErr: tc.finalErr}
			r := &prometheusResource{data: &providerdata.ProviderData{API: api, Locks: &locks.Keyed{}}}
			var schema resource.SchemaResponse
			r.Schema(ctx, resource.SchemaRequest{}, &schema)
			m := model{ID: types.StringValue("p"), ProjectID: types.StringValue("p"), CoreServiceID: types.StringValue("core"), Configs: []configModel{{Targets: []types.String{types.StringValue("localhost:9100")}, Labels: map[string]types.String{}}}, Timeouts: timeouts.Value{Object: types.ObjectNull(map[string]attr.Type{"create": types.StringType, "update": types.StringType, "delete": types.StringType})}}
			state := tfsdk.State{Schema: schema.Schema}
			if d := state.Set(ctx, &m); d.HasError() {
				t.Fatal(d)
			}
			resp := resource.DeleteResponse{State: state}
			r.Delete(ctx, resource.DeleteRequest{State: state}, &resp)
			if resp.Diagnostics.HasError() != tc.bad || !reflect.DeepEqual(api.calls, tc.calls) {
				t.Fatalf("diagnostics=%v calls=%v, want error=%v calls=%v", resp.Diagnostics, api.calls, tc.bad, tc.calls)
			}
			if api.writes != 0 && (api.configs == nil || len(api.configs) != 0) {
				t.Fatalf("reset must write an empty discovery list: %+v", api.configs)
			}
		})
	}
}
