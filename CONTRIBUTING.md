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

**Acceptance tests create real, billable resources.** They are opt-in, must not run in CI, and should normally use a non-production Zerops environment with a dedicated test organization and appropriate integration-token permissions.

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
