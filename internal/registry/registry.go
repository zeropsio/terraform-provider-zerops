// Package registry is the only integration point for resource and data source constructors.
// Resource packages must not import it, so they remain independently buildable.
package registry

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	dslocations "github.com/zeropsio/terraform-provider-zerops/internal/datasources/locations"
	dsorganization "github.com/zeropsio/terraform-provider-zerops/internal/datasources/organization"
	dsproject "github.com/zeropsio/terraform-provider-zerops/internal/datasources/project"
	dsprojectexport "github.com/zeropsio/terraform-provider-zerops/internal/datasources/project_export"
	dsprojects "github.com/zeropsio/terraform-provider-zerops/internal/datasources/projects"
	dsregions "github.com/zeropsio/terraform-provider-zerops/internal/datasources/regions"
	dsservice "github.com/zeropsio/terraform-provider-zerops/internal/datasources/service"
	dsserviceenvvars "github.com/zeropsio/terraform-provider-zerops/internal/datasources/service_env_vars"
	dsservicetypes "github.com/zeropsio/terraform-provider-zerops/internal/datasources/service_types"
	"github.com/zeropsio/terraform-provider-zerops/internal/provider"
	"github.com/zeropsio/terraform-provider-zerops/internal/resources/integration_token"
	"github.com/zeropsio/terraform-provider-zerops/internal/resources/object_storage"
	"github.com/zeropsio/terraform-provider-zerops/internal/resources/project"
	"github.com/zeropsio/terraform-provider-zerops/internal/resources/project_dedicated_ipv4"
	"github.com/zeropsio/terraform-provider-zerops/internal/resources/project_env"
	"github.com/zeropsio/terraform-provider-zerops/internal/resources/project_import"
	"github.com/zeropsio/terraform-provider-zerops/internal/resources/project_l7_balancer_config"
	"github.com/zeropsio/terraform-provider-zerops/internal/resources/project_prometheus_config"
	"github.com/zeropsio/terraform-provider-zerops/internal/resources/project_remote_logging"
	"github.com/zeropsio/terraform-provider-zerops/internal/resources/project_vpn_peer"
	"github.com/zeropsio/terraform-provider-zerops/internal/resources/public_http_routing"
	"github.com/zeropsio/terraform-provider-zerops/internal/resources/public_port_routing"
	"github.com/zeropsio/terraform-provider-zerops/internal/resources/service"
	"github.com/zeropsio/terraform-provider-zerops/internal/resources/service_backup_config"
	"github.com/zeropsio/terraform-provider-zerops/internal/resources/service_deploy"
	"github.com/zeropsio/terraform-provider-zerops/internal/resources/service_env"
	"github.com/zeropsio/terraform-provider-zerops/internal/resources/service_git_integration"
	"github.com/zeropsio/terraform-provider-zerops/internal/resources/service_restart"
	"github.com/zeropsio/terraform-provider-zerops/internal/resources/service_subdomain_access"
)

// New returns the provider's complete registration set.
func New() provider.Registry {
	return provider.Registry{
		Resources: []func() resource.Resource{
			project.NewResource,
			project_env.NewResource,
			service.NewResource,
			service_subdomain_access.NewResource,
			object_storage.NewResource,
			service_env.NewResource,
			public_http_routing.NewResource,
			public_port_routing.NewResource,
			service_deploy.NewResource,
			project_dedicated_ipv4.NewResource,
			project_remote_logging.NewResource,
			project_l7_balancer_config.NewResource,
			project_vpn_peer.NewResource,
			project_import.NewResource,
			integration_token.NewResource,
			project_prometheus_config.NewResource,
			service_backup_config.NewResource,
			service_restart.NewResource,
			service_git_integration.NewResource,
		},
		DataSources: []func() datasource.DataSource{
			dsorganization.NewDataSource,
			dsproject.NewDataSource,
			dsprojects.NewDataSource,
			dsservice.NewDataSource,
			dsserviceenvvars.NewDataSource,
			dsservicetypes.NewDataSource,
			dslocations.NewDataSource,
			dsregions.NewDataSource,
			dsprojectexport.NewDataSource,
		},
	}
}
