package service_git_integration

import (
	"context"
	"errors"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/locks"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
	"testing"
)

func TestSchema(t *testing.T) {
	var resp resource.SchemaResponse
	(&Resource{}).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if ds := resp.Schema.ValidateImplementation(context.Background()); ds.HasError() {
		t.Fatal(ds)
	}
}
func TestEventValidation(t *testing.T) {
	for _, tt := range []struct {
		event       string
		branch, tag types.String
		bad         bool
	}{{"BRANCH", types.StringValue("main"), types.StringNull(), false}, {"BRANCH", types.StringNull(), types.StringNull(), true}, {"TAG", types.StringNull(), types.StringValue("^v.*"), false}, {"TAG", types.StringNull(), types.StringValue("["), true}, {"TAG", types.StringUnknown(), types.StringUnknown(), false}} {
		if err := validate(model{Event: types.StringValue(tt.event), Branch: tt.branch, Tag: tt.tag}); (err != nil) != tt.bad {
			t.Fatalf("event %s: %v", tt.event, err)
		}
	}
}
func TestReadAndProviderSwitch(t *testing.T) {
	m := model{Provider: types.StringValue("github"), Repository: types.StringValue("owner/repo"), Event: types.StringValue("BRANCH"), Branch: types.StringValue("main"), Tag: types.StringNull(), Setup: types.StringNull(), Active: types.BoolValue(true)}
	if m.input().GithubIntegration == nil {
		t.Fatal("github not selected")
	}
	m.Provider = types.StringValue("gitlab")
	v := m.input()
	if v.GitlabIntegration == nil || v.GithubIntegration != nil {
		t.Fatal("provider switch retained old block")
	}
	if err := m.read(v); err != nil || !m.Webhook.IsNull() {
		t.Fatal("nullable readback", err)
	}
	if err := m.read(client.GitIntegrationStatus{}); err == nil {
		t.Fatal("malformed status accepted")
	}
}

type deleteAPI struct {
	client.API
	getErr, resetErr              error
	gets, resets, integrationGets int
}

func (f *deleteAPI) GetService(context.Context, string) (client.Service, error) {
	f.gets++
	return client.Service{}, f.getErr
}
func (f *deleteAPI) SetGitIntegration(_ context.Context, _ string, v client.GitIntegrationStatus) (*client.Process, error) {
	f.resets++
	if f.gets == 0 {
		return nil, errors.New("reset requires live service metadata")
	}
	if v.GithubIntegration != nil || v.GitlabIntegration != nil {
		return nil, errors.New("delete must reset both providers")
	}
	return nil, f.resetErr
}
func (f *deleteAPI) GetGitIntegration(context.Context, string) (client.GitIntegrationStatus, error) {
	f.integrationGets++
	if f.resets == 0 {
		return client.GitIntegrationStatus{}, &client.APIError{HTTPStatus: 400, Code: "githubAuthorizationRequired"}
	}
	return client.GitIntegrationStatus{}, &client.APIError{HTTPStatus: 400, Code: "noExternalRepositoryIntegration"}
}

func TestDeleteExactParentPresence(t *testing.T) {
	for _, tc := range []struct {
		name      string
		getErr    error
		resetErr  error
		fail      bool
		resets    int
		readbacks int
	}{
		{name: "present without OAuth read gate", resets: 1, readbacks: 1},
		{name: "absent", getErr: &client.APIError{HTTPStatus: 400, Code: "serviceStackNotFound"}},
		{name: "wrong status", getErr: &client.APIError{HTTPStatus: 404, Code: "serviceStackNotFound"}, fail: true},
		{name: "permission denied", getErr: &client.APIError{HTTPStatus: 400, Code: "insufficientPermissions"}, fail: true},
		{name: "capability denied", getErr: &client.APIError{HTTPStatus: 400, Code: "githubAuthorizationRequired"}, fail: true},
		{name: "transport failed", getErr: errors.New("transport failed"), fail: true},
		{name: "reset disappearance race", resetErr: &client.APIError{HTTPStatus: 400, Code: "serviceStackNotFound"}, resets: 1},
		{name: "reset failed", resetErr: errors.New("reset failed"), fail: true, resets: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			a := &deleteAPI{getErr: tc.getErr, resetErr: tc.resetErr}
			r := &Resource{data: &providerdata.ProviderData{API: a, Locks: &locks.Keyed{}}}
			var schema resource.SchemaResponse
			r.Schema(ctx, resource.SchemaRequest{}, &schema)
			m := model{ID: types.StringValue("service"), ServiceID: types.StringValue("service"), Timeouts: timeouts.Value{Object: types.ObjectNull(map[string]attr.Type{"create": types.StringType, "update": types.StringType, "delete": types.StringType})}}
			state := tfsdk.State{Schema: schema.Schema}
			if d := state.Set(ctx, &m); d.HasError() {
				t.Fatal(d)
			}
			resp := resource.DeleteResponse{State: state}
			r.Delete(ctx, resource.DeleteRequest{State: state}, &resp)
			if resp.Diagnostics.HasError() != tc.fail {
				t.Fatal(resp.Diagnostics)
			}
			if a.gets != 1 || a.resets != tc.resets || a.integrationGets != tc.readbacks {
				t.Fatalf("parent reads = %d, resets = %d, integration reads = %d; want 1, %d, %d", a.gets, a.resets, a.integrationGets, tc.resets, tc.readbacks)
			}
		})
	}
}
