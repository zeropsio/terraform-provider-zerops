# Contributing

## Prerequisites

- Go 1.27.2, as specified in `go.mod`.
- Terraform or OpenTofu; the target CLI versions are Terraform 1.15.9 and OpenTofu 1.12.1.
- GNU Make and the tools used by the selected make target.

## Local development

```sh
make build       # Build the provider
make test        # Unit tests; live acceptance tests disabled
make generate    # Generate provider reference documentation
make docs-check  # Check generated documentation
make lint        # Run static and example-format checks
```

Update examples and generated documentation when changing public schemas. Pull requests should include relevant tests and a changelog entry. Never commit credentials, Terraform state, or token-bearing logs.

## Acceptance tests

**Acceptance tests create real, billable resources.** They are opt-in and should normally use a non-production Zerops environment with a dedicated test organization and appropriate integration-token permissions. The approved production-small workflow below is the only live CI entry point.

Configure these environment variables before `make testacc`:

| Variable | Non-production configuration |
| --- | --- |
| `ZEROPS_TARGET` | `dev` (also the default when unset or empty) |
| `ZEROPS_ENDPOINT` | Required HTTPS API origin, for example `https://api.dev.example.test` |
| `ZEROPS_DEV_DOMAIN` | Required DNS suffix, for example `dev.example.test` |
| `ZEROPS_TOKEN` | Integration token for the test environment; supply securely |
| `ZEROPS_ORGANIZATION_ID` | Test organization ID, for example `org-test` |
| `ZEROPS_ACC_ENV_FILE` | Optional path to an environment file sourced by the acceptance, sweep, and smoke make targets |

The non-production guard refuses the production endpoint. API requests, signed uploads, and redirects must remain HTTPS and inside `ZEROPS_DEV_DOMAIN` (the domain itself or a true subdomain). Endpoints must not contain credentials, paths, or queries. Values other than empty, `dev`, or `prod` for `ZEROPS_TARGET` are errors.

```sh
export ZEROPS_TARGET=dev
export ZEROPS_ENDPOINT=https://api.dev.example.test
export ZEROPS_DEV_DOMAIN=dev.example.test
export ZEROPS_ORGANIZATION_ID=org-test
# Supply ZEROPS_TOKEN securely, then:
make testacc
```

Production testing requires explicit `ZEROPS_TARGET=prod`, exactly `ZEROPS_ENDPOINT=https://api.app-prg1.zerops.io`, a securely supplied `ZEROPS_TOKEN`, and an explicit `ZEROPS_ORGANIZATION_ID`. The guard verifies that organization against the token's memberships; it does not auto-select one. Also set `ZEROPS_PROD_LEDGER` to the absolute path of a local ownership ledger file. The guard records the resources the run creates there; only those resources may be mutated or deleted. Review costs and cleanup responsibilities before opting in, and retain the file until cleanup is complete.

For provider-level guarded use, `ZEROPS_SANDBOX=dev` or `ZEROPS_SANDBOX=prod` selects the corresponding policy; `dev` also requires `ZEROPS_DEV_DOMAIN`. Unset or empty disables the provider's opt-in guard for normal operation.

### Production-small CI

`.github/workflows/live.yml` runs weekly, on manual dispatch, and on `v*` tags; never on pull requests. A secret-free hosted job accepts only this upstream repository's `main` ref or `v*` tags whose selected commit is an ancestor of `origin/main`. Only its verified SHA is checked out on the `zerops-prod` self-hosted runner, behind the protected `prod-live` environment. Per-repository concurrency does not cancel active runs; host job hooks serialize jobs across repositories.

Configure `ZEROPS_ENDPOINT`, `ZEROPS_TOKEN`, and `ZEROPS_ORGANIZATION_ID` as **prod-live environment secrets**, with the organization set to the dedicated CI organization. Missing values fail closed. The workflow sets `ZEROPS_TARGET=prod`, `ZEROPS_CI_ORGANIZATION_ID` to that same organization secret, an absolute per-run `ZEROPS_PROD_LEDGER`, and `ZEROPS_POSTGRESQL_MIN_DISK_GB=2`. No local environment/credential files are sourced by the workflow.

The exact scope is `TestAccProjectLifecycle`, `TestAccServiceManaged/valkey` (`valkey:single@7.2`), and `TestAccServiceDeployDirectory`. Each runs first with Terraform 1.15.9, then with OpenTofu 1.12.1, serially (`-p 1 -parallel 1`), using these narrow package selectors:

```sh
go test -count=1 -p 1 -parallel 1 ./internal/resources/project -run '^TestAccProjectLifecycle$' -timeout 90m
go test -count=1 -p 1 -parallel 1 ./internal/resources/service -run '^TestAccServiceManaged$/^valkey$' -timeout 90m
go test -count=1 -p 1 -parallel 1 ./internal/resources/service_deploy -run '^TestAccServiceDeployDirectory$' -timeout 90m
```

The workflow supplies `TF_ACC=1` and `TF_ACC_TERRAFORM_PATH` from each setup action's CLI. It does not run the full acceptance suite. Projects retain the existing guard-approved `tfacc-` prefix and services the existing `tfacc` hostname prefix (no hyphens). Projects stay LIGHT, managed services NON_HA, runtime fixtures use one container and catalogue-minimum shared CPU/RAM/disk, and CI lifecycle steps never request IPv4. Local lifecycle coverage is unchanged. PostgreSQL, when tested locally, remains explicitly `postgresql:single@16` with at least 2 GB disk; it is not an extra production-small case.

An `always()` cleanup uses the same ledger and pin, inventories only `tfacc-` projects in the dedicated CI organization, records those exact IDs in the existing ledger, then deletes through the normal ownership guard. This also recovers prior interrupted CI runs; `tfprobe-`, foreign projects, and ledger-only historical entries are not sweep targets. Services disappear with their parent projects, never through hostname-based deletion. Dedicated-IPv4 holders remain protected. A final paginated inventory fails the workflow if any `tfacc-` project remains. Do not use the CI pin for local runs or point it at an organization containing non-CI `tfacc-` projects.

