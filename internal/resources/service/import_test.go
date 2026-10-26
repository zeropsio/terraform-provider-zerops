package service

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
	"testing"
)

type importAPI struct {
	client.API
	service       client.Service
	project, name string
}

func (a *importAPI) GetService(context.Context, string) (client.Service, error) {
	return a.service, nil
}
func (a *importAPI) GetServiceByName(_ context.Context, p, n string) (client.Service, error) {
	a.project = p
	a.name = n
	return a.service, nil
}
func TestImportOwnershipAndCanonicalType(t *testing.T) {
	for _, tt := range []struct {
		name string
		s    client.Service
		bad  bool
	}{{"runtime", client.Service{ID: "s", ProjectID: "p", Name: "app", Base: "ubuntu/nodejs@22"}, false}, {"system", client.Service{IsSystem: true}, true}, {"storage category", client.Service{ServiceTypeCategory: "OBJECT_STORAGE"}, true}, {"storage id", client.Service{ServiceStackTypeID: "object_storage"}, true}, {"storage base", client.Service{Base: "object-storage"}, true}} {
		t.Run(tt.name, func(t *testing.T) {
			api := &importAPI{service: tt.s}
			r := &Resource{data: &providerdata.ProviderData{API: api}}
			var schema resource.SchemaResponse
			r.Schema(context.Background(), resource.SchemaRequest{}, &schema)
			p := resource.ImportStateResponse{State: tfsdk.State{Schema: schema.Schema}}
			r.ImportState(context.Background(), resource.ImportStateRequest{ID: "p/app"}, &p)
			if p.Diagnostics.HasError() != tt.bad {
				t.Fatal(p.Diagnostics)
			}
			if tt.bad {
				return
			}
			var m Model
			d := p.State.Get(context.Background(), &m)
			if d.HasError() {
				t.Fatal(d)
			}
			if m.Type.ValueString() != tt.s.Base || !m.Desired.IsNull() || m.Start.ValueBool() || m.ID.ValueString() != "s" || api.project != "p" || api.name != "app" {
				t.Fatal("import lost canonical type, identity, selector or unmanaged lifecycle")
			}
		})
	}
}
