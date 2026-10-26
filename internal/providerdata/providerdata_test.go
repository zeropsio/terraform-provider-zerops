package providerdata

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"testing"
)

func TestConfigureHelpers(t *testing.T) {
	data := &ProviderData{}
	for _, value := range []any{nil, "wrong", (*ProviderData)(nil), data} {
		var rr resource.ConfigureResponse
		got := FromResourceConfigure(resource.ConfigureRequest{ProviderData: value}, &rr)
		var dr datasource.ConfigureResponse
		other := FromDataSourceConfigure(datasource.ConfigureRequest{ProviderData: value}, &dr)
		invalid := value != nil && value != data
		if rr.Diagnostics.HasError() != invalid || dr.Diagnostics.HasError() != invalid {
			t.Fatalf("unexpected diagnostics for %T: %v %v", value, rr.Diagnostics, dr.Diagnostics)
		}
		if value == data {
			if got != data || other != data {
				t.Fatal("shared pointer not preserved")
			}
		} else if got != nil || other != nil {
			t.Fatal("unexpected payload")
		}
	}
}
