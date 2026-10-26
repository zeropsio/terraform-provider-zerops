package service_deploy

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
)

type readAPI struct {
	client.API
	app                  client.AppVersion
	service              client.Service
	appErr, errorService error
}

func (a readAPI) GetAppVersion(context.Context, string) (client.AppVersion, error) {
	return a.app, a.appErr
}
func (a readAPI) GetService(context.Context, string) (client.Service, error) {
	return a.service, a.errorService
}
func TestReadMapsActiveAndRetainsInputs(t *testing.T) {
	m := model{ID: types.StringValue("app"), ServiceID: types.StringValue("service"), SourceHash: types.StringValue("identity"), ProcessID: types.StringValue("process")}
	r := deployResource{data: &providerdata.ProviderData{API: readAPI{app: client.AppVersion{ID: "app", Status: "BACKUP"}, service: client.Service{ActiveAppVersion: &client.AppVersion{ID: "new-app"}}}}}
	var d diag.Diagnostics
	r.refresh(context.Background(), &m, &d)
	if d.HasError() || m.Active.ValueBool() || m.Status.ValueString() != "BACKUP" || m.SourceHash.ValueString() != "identity" || m.ProcessID.ValueString() != "process" {
		t.Fatalf("invalid mapping: %#v %v", m, d)
	}
}
func TestReadPurgedRetainsInvocation(t *testing.T) {
	m := model{ID: types.StringValue("app"), Active: types.BoolValue(true)}
	r := deployResource{data: &providerdata.ProviderData{API: readAPI{appErr: &client.APIError{HTTPStatus: 400, Code: "appVersionNotFound"}}}}
	var d diag.Diagnostics
	r.refresh(context.Background(), &m, &d)
	if d.HasError() || m.ID.ValueString() != "app" || m.Status.ValueString() != "PURGED" || m.Active.ValueBool() {
		t.Fatalf("invalid purged mapping: %#v %v", m, d)
	}
}
