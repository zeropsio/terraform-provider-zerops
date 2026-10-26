package project

import (
	"context"
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
	"testing"
)

type fakeAPI struct {
	client.API
	ambiguous bool
}

func (f fakeAPI) GetProject(context.Context, string) (client.Project, error) {
	return client.Project{ID: "project", ClientID: "org", Name: "exact", PublicZone: "example.test", TagList: []string{"tag"}}, nil
}
func (f fakeAPI) GetProjectByName(ctx context.Context, org, name string, statuses []string) (client.Project, error) {
	if f.ambiguous {
		return client.Project{}, fmt.Errorf("multiple projects have the exact requested name")
	}
	return f.GetProject(ctx, name)
}
func (f fakeAPI) ListProjectEnv(context.Context, string) ([]client.Env, error) {
	return []client.Env{{Type: "SYSTEM", Key: "envIsolation", Content: "service"}, {Type: "SYSTEM", Key: "sshIsolation", Content: "project"}}, nil
}
func readModel(t *testing.T, api client.API, input model) (model, datasource.ReadResponse) {
	t.Helper()
	ctx := context.Background()
	d := &source{data: &providerdata.ProviderData{API: api, OrganizationID: "org", Endpoint: "https://api.example.test/"}}
	var sr datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &sr)
	state := tfsdk.State{Schema: sr.Schema}
	input.Tags = types.SetNull(types.StringType)
	if diags := state.Set(ctx, &input); diags.HasError() {
		t.Fatal(diags)
	}
	resp := datasource.ReadResponse{State: tfsdk.State{Schema: sr.Schema}}
	d.Read(ctx, datasource.ReadRequest{Config: tfsdk.Config{Schema: sr.Schema, Raw: state.Raw}}, &resp)
	var got model
	if !resp.Diagnostics.HasError() {
		if diags := resp.State.Get(ctx, &got); diags.HasError() {
			t.Fatal(diags)
		}
	}
	return got, resp
}
func TestMapping(t *testing.T) {
	got, resp := readModel(t, fakeAPI{}, model{ID: types.StringValue("project")})
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if got.Name.ValueString() != "exact" || got.EnvIsolation.ValueString() != "service" || got.SSHIsolation.ValueString() != "project" || !got.Description.IsNull() {
		t.Fatalf("bad project mapping: %#v", got)
	}
}
func TestSelectors(t *testing.T) {
	for _, m := range []model{{}, {ID: types.StringValue("p"), Name: types.StringValue("n")}, {ID: types.StringValue("")}, {Name: types.StringValue("")}, {ID: types.StringValue("p"), OrganizationID: types.StringValue("other")}} {
		_, resp := readModel(t, fakeAPI{}, m)
		if !resp.Diagnostics.HasError() {
			t.Fatalf("selector accepted %#v", m)
		}
	}
}
func TestAmbiguity(t *testing.T) {
	_, resp := readModel(t, fakeAPI{ambiguous: true}, model{Name: types.StringValue("exact")})
	if !resp.Diagnostics.HasError() {
		t.Fatal("ambiguous name succeeded")
	}
}
