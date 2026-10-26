package project_remote_logging

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
	client.API
	getErr, writeErr, finalErr error
	calls                      []string
	written                    client.RemoteLoggingWrite
}

func (a *deleteAPI) GetProject(_ context.Context, id string) (client.Project, error) {
	a.calls = append(a.calls, "get:"+id)
	if len(a.calls) == 1 {
		return client.Project{ID: id}, a.getErr
	}
	return client.Project{ID: id}, a.finalErr
}
func (a *deleteAPI) SetRemoteLogging(_ context.Context, id string, v client.RemoteLoggingWrite) (client.Process, error) {
	a.calls = append(a.calls, "reset:"+id)
	a.written = v
	return client.Process{ID: "reset"}, a.writeErr
}
func (a *deleteAPI) GetProcess(context.Context, string) (client.Process, error) {
	a.calls = append(a.calls, "process")
	return client.Process{ID: "reset", Status: "FINISHED"}, nil
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
		{"normal reset", nil, nil, nil, false, []string{"get:p", "reset:p", "process", "get:p"}},
		{"reset absence race", nil, absent, nil, false, []string{"get:p", "reset:p"}},
		{"reset error", nil, denied, nil, true, []string{"get:p", "reset:p"}},
		{"wait absence race", nil, nil, absent, false, []string{"get:p", "reset:p", "process", "get:p"}},
		{"wait read error", nil, nil, denied, true, []string{"get:p", "reset:p", "process", "get:p"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			api := &deleteAPI{getErr: tc.getErr, writeErr: tc.writeErr, finalErr: tc.finalErr}
			r := &configResource{data: &providerdata.ProviderData{API: api, Locks: &locks.Keyed{}}}
			var schema resource.SchemaResponse
			r.Schema(ctx, resource.SchemaRequest{}, &schema)
			m := model{ID: types.StringValue("p"), ProjectID: types.StringValue("p"), SyslogConfig: types.StringValue("secret"), Certificate: types.StringValue("cert"), Type: types.StringValue("syslog"), Timeouts: timeouts.Value{Object: types.ObjectNull(map[string]attr.Type{"create": types.StringType, "update": types.StringType, "delete": types.StringType})}}
			state := tfsdk.State{Schema: schema.Schema}
			if d := state.Set(ctx, &m); d.HasError() {
				t.Fatal(d)
			}
			resp := resource.DeleteResponse{State: state}
			r.Delete(ctx, resource.DeleteRequest{State: state}, &resp)
			if resp.Diagnostics.HasError() != tc.bad || !reflect.DeepEqual(api.calls, tc.calls) {
				t.Fatalf("diagnostics=%v calls=%v, want error=%v calls=%v", resp.Diagnostics, api.calls, tc.bad, tc.calls)
			}
			if len(api.calls) > 1 && (api.written.SyslogConfig != "" || api.written.Certificate != "" || api.written.Type != nil) {
				t.Fatalf("incomplete reset: %+v", api.written)
			}
		})
	}
}
