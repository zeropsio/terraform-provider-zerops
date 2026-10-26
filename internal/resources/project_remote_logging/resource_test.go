package project_remote_logging

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
	"testing"
)

type fakeAPI struct {
	client.API
	project client.Project
	err     error
	written client.RemoteLoggingWrite
}

func (f *fakeAPI) GetProject(context.Context, string) (client.Project, error) {
	return f.project, f.err
}
func (f *fakeAPI) SetRemoteLogging(_ context.Context, _ string, v client.RemoteLoggingWrite) (client.Process, error) {
	f.written = v
	return client.Process{ID: "proc"}, f.err
}
func TestLoggingRetainsSecretsAndMapsType(t *testing.T) {
	typ := "syslog"
	f := &fakeAPI{project: client.Project{LogForwardingType: &typ}}
	r := &configResource{data: &providerdata.ProviderData{API: f}}
	m := model{ProjectID: types.StringValue("p"), SyslogConfig: types.StringValue("secret snippet"), Certificate: types.StringValue("cert")}
	ok, e := r.load(context.Background(), &m)
	if e != nil || !ok || m.SyslogConfig.ValueString() != "secret snippet" || m.Certificate.ValueString() != "cert" || m.Type.ValueString() != "syslog" {
		t.Fatalf("state not retained: %v %v", ok, e)
	}
	m.Type = types.StringNull()
	match, e := r.matches(context.Background(), &m)
	if e != nil || match {
		t.Fatal("mismatched type accepted")
	}
	if _, e = r.write(context.Background(), &m, false); e != nil {
		t.Fatal(e)
	}
	if f.written.Type != nil || f.written.SyslogConfig != "secret snippet" || f.written.Certificate != "cert" {
		t.Fatal("null type must not implicitly clear submitted inputs")
	}
	if _, e = r.write(context.Background(), &m, true); e != nil {
		t.Fatal(e)
	}
	if f.written.Type != nil || f.written.SyslogConfig != "" || f.written.Certificate != "" {
		t.Fatal("incomplete reset")
	}
	imported := model{ProjectID: types.StringValue("p")}
	if _, e = r.load(context.Background(), &imported); e != nil || !imported.SyslogConfig.IsNull() || !imported.Certificate.IsNull() {
		t.Fatal("import invented configuration")
	}
	f.err = &client.APIError{HTTPStatus: 403, Code: "projectNotFound"}
	if _, e = r.load(context.Background(), &m); e == nil {
		t.Fatal("auth removed state")
	}
	f.err = &client.APIError{HTTPStatus: 400, Code: "projectNotFound"}
	if exists, e := r.load(context.Background(), &m); exists || e != nil {
		t.Fatal("exact absence not removed")
	}
}
