package registry

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/zeropsio/terraform-provider-zerops/internal/provider"
)

// TestProviderSchema validates every registered schema (framework rejects e.g. Default
// without Computed) and pins the v1 type inventory.
func TestProviderSchema(t *testing.T) {
	srv, err := providerserver.NewProtocol6WithError(provider.New("test", New())())()
	if err != nil {
		t.Fatal(err)
	}
	resp, err := srv.GetProviderSchema(context.Background(), &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range resp.Diagnostics {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			t.Errorf("schema diagnostic: %s: %s", d.Summary, d.Detail)
		}
	}
	wantResources := []string{
		"zerops_project", "zerops_project_env", "zerops_service", "zerops_service_subdomain_access",
		"zerops_object_storage", "zerops_service_env", "zerops_public_http_routing",
		"zerops_public_port_routing", "zerops_service_deploy",
		"zerops_project_dedicated_ipv4", "zerops_service_backup_config", "zerops_service_restart",
		"zerops_project_import", "zerops_project_remote_logging", "zerops_project_l7_balancer_config",
		"zerops_project_prometheus_config", "zerops_project_vpn_peer", "zerops_integration_token",
		"zerops_service_git_integration",
	}
	wantData := []string{
		"zerops_organization", "zerops_project", "zerops_projects", "zerops_service",
		"zerops_service_env_vars", "zerops_service_types", "zerops_locations", "zerops_regions",
		"zerops_project_export",
	}
	for _, n := range wantResources {
		if _, ok := resp.ResourceSchemas[n]; !ok {
			t.Errorf("resource %s not registered", n)
		}
	}
	for _, n := range wantData {
		if _, ok := resp.DataSourceSchemas[n]; !ok {
			t.Errorf("data source %s not registered", n)
		}
	}
	if len(resp.ResourceSchemas) != len(wantResources) || len(resp.DataSourceSchemas) != len(wantData) {
		t.Errorf("got %d resources / %d data sources", len(resp.ResourceSchemas), len(resp.DataSourceSchemas))
	}
}
