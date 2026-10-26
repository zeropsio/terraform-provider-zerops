package projects

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
	"testing"
)

type fakeAPI struct {
	client.API
	t     *testing.T
	calls int
}

func (f *fakeAPI) ListProjects(_ context.Context, org string, q client.ProjectQuery) (client.Page[client.Project], error) {
	f.calls++
	if org != "org" || len(q.Statuses) != 0 {
		f.t.Fatal("wrong organization or server-side status filter")
	}
	if q.Offset == 0 {
		return client.Page[client.Project]{Items: []client.Project{{ID: "z", Name: "app", Status: "ACTIVE", TagList: []string{"tag"}}}, Total: 2}, nil
	}
	return client.Page[client.Project]{Items: []client.Project{{ID: "a", Name: "app", Status: "STOPPED", TagList: []string{}}}, Total: 2, Offset: q.Offset}, nil
}
func TestReadPaginationAndMapping(t *testing.T) {
	ctx := context.Background()
	api := &fakeAPI{t: t}
	d := &source{data: &providerdata.ProviderData{API: api, OrganizationID: "org"}}
	var sr datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &sr)
	statuses, diags := types.SetValueFrom(ctx, types.StringType, []string{"ACTIVE"})
	if diags.HasError() {
		t.Fatal(diags)
	}
	input := model{Statuses: statuses}
	state := tfsdk.State{Schema: sr.Schema}
	if diags := state.Set(ctx, &input); diags.HasError() {
		t.Fatal(diags)
	}
	resp := datasource.ReadResponse{State: tfsdk.State{Schema: sr.Schema}}
	d.Read(ctx, datasource.ReadRequest{Config: tfsdk.Config{Schema: sr.Schema, Raw: state.Raw}}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	var got model
	if diags := resp.State.Get(ctx, &got); diags.HasError() {
		t.Fatal(diags)
	}
	if api.calls != 2 || len(got.Projects) != 1 || got.Projects[0].ID.ValueString() != "z" || !got.Projects[0].Location.IsNull() || !got.Projects[0].Description.IsNull() || len(got.Projects[0].Tags.Elements()) != 1 {
		t.Fatalf("bad pagination or mapping: %#v calls %d", got, api.calls)
	}
}
