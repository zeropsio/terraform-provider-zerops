# Changelog

## 0.1.2

2026-10-30

### Breaking change

- `zerops_service` now rejects, at plan time, an unqualified `type` for managed families that offer both single-node and highly available modes, for example `postgresql@16`. Zerops resolves that shorthand to an HA service. Use `postgresql:single@16` (single node, NON_HA) or `postgresql:ha@16` (HA). Runtime types, single-only and HA-only families, and unknown values are unaffected.
- `zerops_project_import` applies the same check to `services[].type` in known YAML, unless the entry sets the deprecated `mode` key.
- The set of dual-mode families is derived from the live service catalog. If the catalog cannot be read, a built-in snapshot is used: postgresql, mariadb, valkey, clickhouse, elasticsearch, kafka, nats, qdrant, seaweedfs and typesense.

### Upgrading

- A service created with a shorthand by v0.1.0/v0.1.1 keeps working after you change `type` to the qualified value in its `base` attribute (for example `postgresql@16` → `postgresql:ha@16`). That change updates state only and does not replace the service. Choosing the other mode still replaces it.

### Other

- `zerops_service` adds plan-time RAM floors that, like the PostgreSQL disk floor, default to the floor value and reject smaller configured values: ClickHouse 0.375 GB (`ZEROPS_CLICKHOUSE_MIN_RAM_GB`) and Elasticsearch 2 GB (`ZEROPS_ELASTICSEARCH_MIN_RAM_GB`). Smaller sizes fail to provision on Zerops. `vertical_autoscaling.min_ram_gb` and `max_ram_gb` are now Optional+Computed so that these defaults can be planned.
- Examples and documentation use explicit managed types.
- Acceptance tests cover every creatable managed family at its provisionable minimum size.

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
