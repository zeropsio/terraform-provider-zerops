package provider_test

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	framework "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/zeropsio/terraform-provider-zerops/provider"
)

func TestNew(t *testing.T) {
	ctx := context.Background()
	p := provider.New("0.1.1", provider.WithUserAgent("pulumi-zerops/0.1.0"))
	var metadata framework.MetadataResponse
	p.Metadata(ctx, framework.MetadataRequest{}, &metadata)
	if metadata.TypeName != "zerops" || metadata.Version != "0.1.1" {
		t.Fatalf("unexpected metadata: %#v", metadata)
	}
	wantResources := map[string]bool{
		"zerops_integration_token": true, "zerops_object_storage": true,
		"zerops_project": true, "zerops_project_dedicated_ipv4": true,
		"zerops_project_env": true, "zerops_project_import": true,
		"zerops_project_l7_balancer_config": true, "zerops_project_prometheus_config": true,
		"zerops_project_remote_logging": true, "zerops_project_vpn_peer": true,
		"zerops_public_http_routing": true, "zerops_public_port_routing": true,
		"zerops_service": true, "zerops_service_backup_config": true,
		"zerops_service_deploy": true, "zerops_service_env": true,
		"zerops_service_git_integration": true, "zerops_service_restart": true,
		"zerops_service_subdomain_access": true,
	}
	resources := p.Resources(ctx)
	if len(resources) != len(wantResources) {
		t.Fatalf("got %d resources, want %d", len(resources), len(wantResources))
	}
	for _, constructor := range resources {
		var response resource.MetadataResponse
		constructor().Metadata(ctx, resource.MetadataRequest{ProviderTypeName: metadata.TypeName}, &response)
		if !wantResources[response.TypeName] {
			t.Fatalf("unexpected or duplicate resource %q", response.TypeName)
		}
		delete(wantResources, response.TypeName)
	}
	wantDataSources := map[string]bool{
		"zerops_locations": true, "zerops_organization": true, "zerops_project": true,
		"zerops_project_export": true, "zerops_projects": true, "zerops_regions": true,
		"zerops_service": true, "zerops_service_env_vars": true, "zerops_service_types": true,
	}
	dataSources := p.DataSources(ctx)
	if len(dataSources) != len(wantDataSources) {
		t.Fatalf("got %d data sources, want %d", len(dataSources), len(wantDataSources))
	}
	for _, constructor := range dataSources {
		var response datasource.MetadataResponse
		constructor().Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: metadata.TypeName}, &response)
		if !wantDataSources[response.TypeName] {
			t.Fatalf("unexpected or duplicate data source %q", response.TypeName)
		}
		delete(wantDataSources, response.TypeName)
	}
}

func TestWithUserAgentValidation(t *testing.T) {
	for _, product := range []string{"", " ", " product", "product ", "product name", "product\tname", "product\nname", "product\rname", "product\x00name", "product\x1fname", "product\x7fname", "product\u0085name", "product\u00a0name", "product\u2003name"} {
		t.Run(product, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("WithUserAgent(%q) did not panic", product)
				}
			}()
			provider.WithUserAgent(product)
		})
	}
	for _, product := range []string{"pulumi-zerops/0.1.0", "embedding-product"} {
		if provider.WithUserAgent(product) == nil {
			t.Fatalf("WithUserAgent(%q) returned nil", product)
		}
	}
}
