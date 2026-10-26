# Zerops Terraform Provider

Manage Zerops projects, services, deployments, storage, and networking with Terraform or OpenTofu.

**Status:** The Zerops public API is beta, and this provider is pre-1.0. Breaking changes are possible in 0.x releases.

## Requirements

- Terraform 1.x or OpenTofu 1.x (tested with Terraform 1.15.9 and OpenTofu 1.12.1).
- A CLI supporting Terraform provider protocol 6. Older CLI compatibility has not been established.
- A Zerops account and an integration token with permissions for the resources you manage.

## Installation

Use the same configuration with Terraform and OpenTofu:

```hcl
terraform {
  required_providers {
    zerops = {
      source  = "zeropsio/zerops"
      version = "~> 0.1"
    }
  }
}

provider "zerops" {}
```

Run `terraform init` or `tofu init` to install the provider.

## Authentication

Set `ZEROPS_TOKEN` to your integration token, or configure the sensitive `token` provider attribute. Keep credentials out of version control.

| Provider attribute | Environment variable | Default |
| --- | --- | --- |
| `token` | `ZEROPS_TOKEN` | Required |
| `organization_id` | `ZEROPS_ORGANIZATION_ID` | Automatically selected only when the token has exactly one organization membership |
| `endpoint` | `ZEROPS_ENDPOINT` | `https://api.app-prg1.zerops.io` |
| `postgresql_min_disk_gb` | `ZEROPS_POSTGRESQL_MIN_DISK_GB` | `2` GB; PostgreSQL disk default and minimum |

Explicit provider attributes take precedence over environment variables. The endpoint must be an HTTPS origin. Some sensitive reads require sudo-enabled credentials; the provider does not perform privilege escalation. Sensitive values remain in Terraform state: protect state files and backend access.

## Quick example

With authentication configured and the installation block above:

```hcl
resource "zerops_project" "example" {
  name               = "example"
  description        = "Managed by Terraform"
  mode               = "LIGHT"
  env_isolation      = "none"
  ssh_isolation      = "vpn"
  public_ipv4_shared = false
}

resource "zerops_service" "app" {
  project_id         = zerops_project.example.id
  hostname           = "app"
  type               = "nodejs@22"
  start_without_code = true
  desired_state      = "RUNNING"
}
```

This creates a project and a service ready for a later deployment; it does not deploy application code. See [examples](examples/) for deployment and configuration examples.

## Resources and data sources

Full schemas and import instructions are available in the [Registry documentation](https://registry.terraform.io/providers/zeropsio/zerops/latest/docs).

### Resources

- [zerops_integration_token](https://registry.terraform.io/providers/zeropsio/zerops/latest/docs/resources/integration_token)
- [zerops_object_storage](https://registry.terraform.io/providers/zeropsio/zerops/latest/docs/resources/object_storage)
- [zerops_project](https://registry.terraform.io/providers/zeropsio/zerops/latest/docs/resources/project)
- [zerops_project_dedicated_ipv4](https://registry.terraform.io/providers/zeropsio/zerops/latest/docs/resources/project_dedicated_ipv4)
- [zerops_project_env](https://registry.terraform.io/providers/zeropsio/zerops/latest/docs/resources/project_env)
- [zerops_project_import](https://registry.terraform.io/providers/zeropsio/zerops/latest/docs/resources/project_import)
- [zerops_project_l7_balancer_config](https://registry.terraform.io/providers/zeropsio/zerops/latest/docs/resources/project_l7_balancer_config)
- [zerops_project_prometheus_config](https://registry.terraform.io/providers/zeropsio/zerops/latest/docs/resources/project_prometheus_config)
- [zerops_project_remote_logging](https://registry.terraform.io/providers/zeropsio/zerops/latest/docs/resources/project_remote_logging)
- [zerops_project_vpn_peer](https://registry.terraform.io/providers/zeropsio/zerops/latest/docs/resources/project_vpn_peer)
- [zerops_public_http_routing](https://registry.terraform.io/providers/zeropsio/zerops/latest/docs/resources/public_http_routing)
- [zerops_public_port_routing](https://registry.terraform.io/providers/zeropsio/zerops/latest/docs/resources/public_port_routing)
- [zerops_service](https://registry.terraform.io/providers/zeropsio/zerops/latest/docs/resources/service)
- [zerops_service_backup_config](https://registry.terraform.io/providers/zeropsio/zerops/latest/docs/resources/service_backup_config)
- [zerops_service_deploy](https://registry.terraform.io/providers/zeropsio/zerops/latest/docs/resources/service_deploy)
- [zerops_service_env](https://registry.terraform.io/providers/zeropsio/zerops/latest/docs/resources/service_env)
- [zerops_service_git_integration](https://registry.terraform.io/providers/zeropsio/zerops/latest/docs/resources/service_git_integration)
- [zerops_service_restart](https://registry.terraform.io/providers/zeropsio/zerops/latest/docs/resources/service_restart)
- [zerops_service_subdomain_access](https://registry.terraform.io/providers/zeropsio/zerops/latest/docs/resources/service_subdomain_access)

### Data sources

- [zerops_locations](https://registry.terraform.io/providers/zeropsio/zerops/latest/docs/data-sources/locations)
- [zerops_organization](https://registry.terraform.io/providers/zeropsio/zerops/latest/docs/data-sources/organization)
- [zerops_project](https://registry.terraform.io/providers/zeropsio/zerops/latest/docs/data-sources/project)
- [zerops_project_export](https://registry.terraform.io/providers/zeropsio/zerops/latest/docs/data-sources/project_export)
- [zerops_projects](https://registry.terraform.io/providers/zeropsio/zerops/latest/docs/data-sources/projects)
- [zerops_regions](https://registry.terraform.io/providers/zeropsio/zerops/latest/docs/data-sources/regions)
- [zerops_service](https://registry.terraform.io/providers/zeropsio/zerops/latest/docs/data-sources/service)
- [zerops_service_env_vars](https://registry.terraform.io/providers/zeropsio/zerops/latest/docs/data-sources/service_env_vars)
- [zerops_service_types](https://registry.terraform.io/providers/zeropsio/zerops/latest/docs/data-sources/service_types)

## Known limitations

- PostgreSQL defaults to a 2 GB disk floor, enforced during planning because the platform currently fails to provision PostgreSQL below it (generic HTTP 500 error). Configure the floor with `postgresql_min_disk_gb` / `ZEROPS_POSTGRESQL_MIN_DISK_GB` when the catalog changes.
- Project credit limits cannot be read back reliably; changing or removing a configured limit can prevent later project updates.
- Avoid reserved service hostnames such as `core` and `l7httpbalancer`; they may interfere with project operation and deletion.
- The API's server-side Git builds may ignore tag or commit references and reject private repositories. Consult the deployment resource documentation for provider-side Git deployment options.
- API failures can leave partially created resources that require manual cleanup in the Zerops dashboard.

## Development

```sh
make build
make test
make docs-check
make lint
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for prerequisites, documentation generation, and guarded acceptance tests.

## License

[MIT](LICENSE). Copyright (c) 2026 Zerops s.r.o.
