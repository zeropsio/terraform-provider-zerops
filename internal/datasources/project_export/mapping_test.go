package project_export

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
)

type fakeAPI struct {
	client.API
	reveal *bool
}

func (f fakeAPI) ExportProject(_ context.Context, id string, reveal bool) (string, error) {
	*f.reveal = reveal
	return "project: example", nil
}
func readModel(t *testing.T, api client.API, input model) (model, datasource.ReadResponse) {
	t.Helper()
	ctx := context.Background()
	d := &source{data: &providerdata.ProviderData{API: api, OrganizationID: "org", Endpoint: "https://api.example.test/"}}
	var sr datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &sr)
	state := tfsdk.State{Schema: sr.Schema}
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
	for _, reveal := range []types.Bool{types.BoolNull(), types.BoolValue(true)} {
		seen := false
		got, resp := readModel(t, fakeAPI{reveal: &seen}, model{ProjectID: types.StringValue("project"), Reveal: reveal})
		if resp.Diagnostics.HasError() {
			t.Fatal(resp.Diagnostics)
		}
		if got.ID.ValueString() != "project" || got.YAML.ValueString() == "" || seen != reveal.ValueBool() {
			t.Fatal("bad export mapping or reveal default")
		}
	}
}
