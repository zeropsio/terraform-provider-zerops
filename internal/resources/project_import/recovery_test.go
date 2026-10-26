package project_import

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
)

type failedAPI struct {
	client.API
	imports int
	hidden  bool
}

func (a *failedAPI) ImportProject(context.Context, string, string) (client.ProjectImport, error) {
	a.imports++
	processes := []client.Process{{ID: "q", Status: "FAILED"}}
	if a.hidden {
		processes = nil
	}
	return client.ProjectImport{ProjectID: "p", ProjectName: "name", ServiceStacks: []client.ProjectImportService{{ID: "s", Name: "cache", Processes: processes}}}, nil
}
func (a *failedAPI) GetProcess(context.Context, string) (client.Process, error) {
	return client.Process{ID: "q", Status: "FAILED", Error: &client.ProcessError{Code: "failure", Message: "recipe-secret"}}, nil
}
func (a *failedAPI) ListProjectProcesses(context.Context, string, client.PageQuery) (client.Page[client.Process], error) {
	return client.Page[client.Process]{Items: []client.Process{{ID: "hidden", ActionName: "project.create", Status: "FAILED"}}}, nil
}
func TestCreateRetainsIdentityBeforeReturnedOrHiddenFailure(t *testing.T) {
	for _, hidden := range []bool{false, true} {
		ctx := context.Background()
		api := &failedAPI{hidden: hidden}
		r := Resource{data: &providerdata.ProviderData{API: api, OrganizationID: "org"}}
		var sr resource.SchemaResponse
		r.Schema(ctx, resource.SchemaRequest{}, &sr)
		m := Model{OrganizationID: types.StringValue("org"), YAML: types.StringValue("project: {name: name}\nservices: [{hostname: cache, type: 'valkey:single@7.2'}]"), Services: types.MapUnknown(types.StringType), Timeouts: timeouts.Value{Object: types.ObjectNull(map[string]attr.Type{"create": types.StringType, "update": types.StringType, "delete": types.StringType})}}
		plan := tfsdk.Plan{Schema: sr.Schema}
		if d := plan.Set(ctx, &m); d.HasError() {
			t.Fatal(d)
		}
		response := resource.CreateResponse{State: tfsdk.State{Schema: sr.Schema}}
		r.Create(ctx, resource.CreateRequest{Plan: plan}, &response)
		if !response.Diagnostics.HasError() {
			t.Fatal("failure ignored")
		}
		var saved Model
		if d := response.State.Get(ctx, &saved); d.HasError() {
			t.Fatal(d)
		}
		if saved.ID.ValueString() != "p" || saved.ProjectID.ValueString() != "p" || saved.Services.Elements()["cache"] != types.StringValue("s") || api.imports != 1 {
			t.Fatal("identity lost or mutation replayed")
		}
	}
}
