package project_dedicated_ipv4

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/locks"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
	"strings"
	"testing"
)

type fakeAPI struct {
	client.API
	address               *string
	readErr, errorRequest error
	requests              int
	processStatus         string
}

func (f *fakeAPI) GetProject(context.Context, string) (client.Project, error) {
	return client.Project{ID: "project", PublicIPv4: f.address}, f.readErr
}
func (f *fakeAPI) RequestDedicatedIPv4(context.Context, string) (client.Process, error) {
	f.requests++
	return client.Process{ID: "process"}, f.errorRequest
}
func (f *fakeAPI) GetProcess(context.Context, string) (client.Process, error) {
	if f.processStatus == "FINISHED" {
		a := "192.0.2.8"
		f.address = &a
	}
	return client.Process{ID: "process", Status: f.processStatus}, nil
}
func setup(t *testing.T, api *fakeAPI) (*Resource, tfsdk.Plan, tfsdk.State) {
	t.Helper()
	ctx := context.Background()
	r := &Resource{data: &providerdata.ProviderData{API: api, Locks: &locks.Keyed{}}}
	var sr resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sr)
	m := model{ProjectID: types.StringValue("project"), ID: types.StringUnknown(), Address: types.StringUnknown(), Timeouts: timeouts.Value{Object: types.ObjectNull(map[string]attr.Type{"create": types.StringType, "update": types.StringType, "delete": types.StringType})}}
	plan := tfsdk.Plan{Schema: sr.Schema}
	if d := plan.Set(ctx, &m); d.HasError() {
		t.Fatal(d)
	}
	return r, plan, tfsdk.State{Schema: sr.Schema}
}
func TestCreate(t *testing.T) {
	for _, mode := range []string{"adopt", "allocate", "forbidden", "conflict", "failed", "auth", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			api := &fakeAPI{processStatus: "FINISHED"}
			switch mode {
			case "adopt":
				a := "192.0.2.8"
				api.address = &a
			case "forbidden":
				api.errorRequest = &client.APIError{HTTPStatus: 400, Code: "publicIpRequestForbidden"}
			case "conflict":
				api.errorRequest = &client.APIError{HTTPStatus: 400, Code: "publicIpRequestOnlyOne"}
			case "failed":
				api.processStatus = "FAILED"
			case "auth":
				api.readErr = &client.APIError{HTTPStatus: 401, Code: "notAuthorized"}
			case "malformed":
				a := "not-an-ip"
				api.address = &a
			}
			r, plan, state := setup(t, api)
			resp := resource.CreateResponse{State: state}
			r.Create(context.Background(), resource.CreateRequest{Plan: plan}, &resp)
			success := mode == "adopt" || mode == "allocate"
			if resp.Diagnostics.HasError() == success {
				t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
			}
			if mode == "adopt" || mode == "auth" || mode == "malformed" {
				if api.requests != 0 {
					t.Fatal("unexpected allocation")
				}
			}
			if mode == "forbidden" || mode == "conflict" {
				if !resp.State.Raw.IsNull() {
					t.Fatal("failed request left state")
				}
			}
			if success || mode == "failed" {
				var m model
				if d := resp.State.Get(context.Background(), &m); d.HasError() {
					t.Fatal(d)
				}
				if m.ID.ValueString() != "project" {
					t.Fatal("lost identity")
				}
			}
		})
	}
}
func TestReadImportDelete(t *testing.T) {
	ctx := context.Background()
	a := "192.0.2.8"
	api := &fakeAPI{address: &a}
	r, plan, state := setup(t, api)
	create := resource.CreateResponse{State: state}
	r.Create(ctx, resource.CreateRequest{Plan: plan}, &create)
	state = create.State
	for _, err := range []error{&client.APIError{HTTPStatus: 401, Code: "notAuthorized"}, &client.APIError{HTTPStatus: 404, Code: "projectNotFound"}} {
		api.readErr = err
		resp := resource.ReadResponse{State: state}
		r.Read(ctx, resource.ReadRequest{State: state}, &resp)
		if !resp.Diagnostics.HasError() || resp.State.Raw.IsNull() {
			t.Fatal("nonabsence removed state")
		}
	}
	api.readErr = nil
	api.address = nil
	read := resource.ReadResponse{State: state}
	r.Read(ctx, resource.ReadRequest{State: state}, &read)
	if !read.State.Raw.IsNull() || len(read.Diagnostics) == 0 {
		t.Fatal("absent allocation retained")
	}
	imp := resource.ImportStateResponse{State: tfsdk.State{Schema: state.Schema}}
	r.ImportState(ctx, resource.ImportStateRequest{ID: "project"}, &imp)
	if !imp.Diagnostics.HasError() {
		t.Fatal("import allocated")
	}
	api.address = &a
	imp = resource.ImportStateResponse{State: tfsdk.State{Schema: state.Schema}}
	r.ImportState(ctx, resource.ImportStateRequest{ID: "project"}, &imp)
	if imp.Diagnostics.HasError() {
		t.Fatal(imp.Diagnostics)
	}
	del := resource.DeleteResponse{State: state}
	r.Delete(ctx, resource.DeleteRequest{State: state}, &del)
	if del.Diagnostics.HasError() || len(del.Diagnostics) != 1 || !strings.Contains(del.Diagnostics[0].Detail(), "remains billed") || !del.State.Raw.IsNull() || api.requests != 0 {
		t.Fatal("incorrect non-releasing delete")
	}
}
