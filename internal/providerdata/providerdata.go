// Package providerdata holds the shared provider configuration without registration dependencies.
package providerdata

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/locks"
)

// ProviderData is shared by all resource and data source instances of a provider.
type ProviderData struct {
	API                   client.API
	Locks                 *locks.Keyed
	OrganizationID        string
	Endpoint              string
	RoutingForceSync      bool
	PostgresqlMinDiskGB   float64
	ClickhouseMinRAMGB    float64
	ElasticsearchMinRAMGB float64
}

// FromResourceConfigure accepts unconfigured providers and diagnoses incompatible payloads.
func FromResourceConfigure(req resource.ConfigureRequest, resp *resource.ConfigureResponse) *ProviderData {
	if req.ProviderData == nil {
		return nil
	}
	data, ok := req.ProviderData.(*ProviderData)
	if !ok || data == nil {
		resp.Diagnostics.AddError("Unexpected provider configuration", "Expected *providerdata.ProviderData. Please report this provider implementation error.")
		return nil
	}
	return data
}

// FromDataSourceConfigure accepts unconfigured providers and diagnoses incompatible payloads.
func FromDataSourceConfigure(req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) *ProviderData {
	if req.ProviderData == nil {
		return nil
	}
	data, ok := req.ProviderData.(*ProviderData)
	if !ok || data == nil {
		resp.Diagnostics.AddError("Unexpected provider configuration", "Expected *providerdata.ProviderData. Please report this provider implementation error.")
		return nil
	}
	return data
}
