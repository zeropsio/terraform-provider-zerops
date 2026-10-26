package project

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
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
)

type deleteAPI struct {
	client.API
	getErr, deleteErr, finalErr error
	processStatus               string
	calls                       []string
}

func (a *deleteAPI) GetProject(context.Context, string) (client.Project, error) {
	a.calls = append(a.calls, "get")
	if len(a.calls) == 1 {
		return client.Project{ID: "p"}, a.getErr
	}
	if a.finalErr != nil {
		return client.Project{}, a.finalErr
	}
	return client.Project{}, &client.APIError{HTTPStatus: 400, Code: "projectNotFound"}
}
func (a *deleteAPI) DeleteProject(context.Context, string) (client.Process, error) {
	a.calls = append(a.calls, "delete")
	return client.Process{ID: "delete"}, a.deleteErr
}
func (a *deleteAPI) GetProcess(context.Context, string) (client.Process, error) {
	a.calls = append(a.calls, "process")
	return client.Process{ID: "delete", Status: a.processStatus}, nil
}

func TestDeletePreflightAndWaits(t *testing.T) {
	absent := &client.APIError{HTTPStatus: 400, Code: "projectNotFound"}
	denied := &client.APIError{HTTPStatus: 403, Code: "forbidden"}
	for _, tc := range []struct {
		name                        string
		getErr, deleteErr, finalErr error
		status                      string
		bad                         bool
		calls                       []string
	}{
		{"already absent", absent, nil, nil, "FINISHED", false, []string{"get"}},
		{"unexpected get error", errors.New("lookup failed"), nil, nil, "FINISHED", true, []string{"get"}},
		{"transport get error", &client.TransportError{}, nil, nil, "FINISHED", true, []string{"get"}},
		{"permission denied", denied, nil, nil, "FINISHED", true, []string{"get"}},
		{"transient get error", &client.APIError{HTTPStatus: 503, Code: "unavailable"}, nil, nil, "FINISHED", true, []string{"get"}},
		{"generic not found", &client.APIError{HTTPStatus: 404, Code: "notFound"}, nil, nil, "FINISHED", true, []string{"get"}},
		{"wrong absence code", &client.APIError{HTTPStatus: 400, Code: "serviceStackNotFound"}, nil, nil, "FINISHED", true, []string{"get"}},
		{"normal delete", nil, nil, nil, "FINISHED", false, []string{"get", "delete", "process", "get"}},
		{"delete already absent", nil, absent, nil, "FINISHED", false, []string{"get", "delete"}},
		{"delete error", nil, denied, nil, "FINISHED", true, []string{"get", "delete"}},
		{"failed process", nil, nil, nil, "FAILED", true, []string{"get", "delete", "process"}},
		{"absence lookup error", nil, nil, denied, "FINISHED", true, []string{"get", "delete", "process", "get"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			api := &deleteAPI{getErr: tc.getErr, deleteErr: tc.deleteErr, finalErr: tc.finalErr, processStatus: tc.status}
			r := Resource{data: &providerdata.ProviderData{API: api}}
			var schema resource.SchemaResponse
			r.Schema(ctx, resource.SchemaRequest{}, &schema)
			m := Model{ID: types.StringValue("p"), Tags: types.SetNull(types.StringType), Timeouts: timeouts.Value{Object: types.ObjectNull(map[string]attr.Type{"create": types.StringType, "update": types.StringType, "delete": types.StringType})}}
			state := tfsdk.State{Schema: schema.Schema}
			if d := state.Set(ctx, &m); d.HasError() {
				t.Fatal(d)
			}
			response := resource.DeleteResponse{State: state}
			r.Delete(ctx, resource.DeleteRequest{State: state}, &response)
			if response.Diagnostics.HasError() != tc.bad || !reflect.DeepEqual(api.calls, tc.calls) {
				t.Fatalf("diagnostics=%v calls=%v, want error=%v calls=%v", response.Diagnostics, api.calls, tc.bad, tc.calls)
			}
		})
	}
}
