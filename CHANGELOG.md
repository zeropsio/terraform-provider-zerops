# Changelog

## 0.1.1

2026-10-29

- Expose a public `provider` Go package for embedding the fully registered provider, for example in the Pulumi bridge.
- Add a User-Agent product option for embedding applications; the default Terraform User-Agent is unchanged.
- No resource or data source behavior changes.

## v0.1.0

2026-10-26

Initial public release of the `zeropsio/zerops` provider for Terraform and OpenTofu, using provider protocol 6.

### Resources

- Manage projects, project environment variables, and YAML project imports.
- Configure dedicated IPv4 addresses, HTTP and port routing, load balancing, remote logging, Prometheus, and VPN peers.
- Manage services, service environment variables, deployments, Git integrations, backups, restarts, and subdomain access.
- Manage object storage and integration tokens.

### Data sources

- Look up organizations, individual projects and services, and project lists.
- Discover locations, regions, and available service types.
- Export project YAML and read service environment variables with appropriate credentials.

### Provider behavior

- Configure authentication, organization selection, and the API endpoint through provider attributes or environment variables.
- Default and enforce a configurable PostgreSQL disk floor of 2 GB with `postgresql_min_disk_gb` / `ZEROPS_POSTGRESQL_MIN_DISK_GB`, preventing provisioning failures from undersized disks.
- Support resource import where documented, asynchronous operation waits, and configurable operation timeouts.
- Include examples, generated reference documentation, unit tests, and opt-in guarded acceptance tests.

The Zerops public API is beta and the provider is pre-1.0. See the README's known limitations before use; breaking changes are possible in 0.x releases.
