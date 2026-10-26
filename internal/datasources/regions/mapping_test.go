package regions

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
)

type fakeAPI struct{ client.API }

func (f fakeAPI) Regions(context.Context) ([]client.Region, error) {
	return []client.Region{{Name: "z"}, {Name: "a", Address: "a.example.test", IsDefault: true}}, nil
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
	got, resp := readModel(t, fakeAPI{}, model{})
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if got.ID.ValueString() != "https://api.example.test" || len(got.Items) != 2 || got.Items[0].Name.ValueString() != "a" || !got.Items[0].IsDefault.ValueBool() {
		t.Fatalf("bad sorted regions: %#v", got)
	}
}
