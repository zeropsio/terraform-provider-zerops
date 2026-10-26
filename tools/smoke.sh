#!/usr/bin/env bash
set +x
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
if [[ -f .goenv ]]; then source .goenv; fi
source tools/target-guard.sh
zerops_target_precheck
zerops_production_run_lock
root="$PWD"
export CHECKPOINT_DISABLE=1 TF_IN_AUTOMATION=1
export TF_VAR_zerops_endpoint="$ZEROPS_ENDPOINT"
export TF_VAR_production=false
[[ "${ZEROPS_TARGET:-}" != prod ]] || export TF_VAR_production=true
export TF_CLI_CONFIG_FILE="$root/.dev.tfrc"
[[ -d examples/smoke ]] || { echo 'examples/smoke is required' >&2; exit 1; }
[[ -x bin/terraform-provider-zerops ]] || { echo 'Run make build first' >&2; exit 1; }
# `zeropsio/zerops` resolves to registry.terraform.io under Terraform and to
# registry.opentofu.org under OpenTofu; serve the local build under both.
hosts=(registry.terraform.io registry.opentofu.org)
cat > "$TF_CLI_CONFIG_FILE" <<EOF
provider_installation {
  dev_overrides {
    "registry.terraform.io/zeropsio/zerops" = "$root/bin"
    "registry.opentofu.org/zeropsio/zerops" = "$root/bin"
  }
  direct {}
}
EOF
# No registry discovery: init uses only a workspace filesystem mirror.
arch="$(go env GOOS)_$(go env GOARCH)"
mirror="$root/.smoke/mirror"
# Terraform never selects version 0.0.0 from a mirror ("no available releases"), so use 0.1.0.
for host in "${hosts[@]}"; do
  mkdir -p "$mirror/$host/zeropsio/zerops/0.1.0/$arch"
  cp bin/terraform-provider-zerops "$mirror/$host/zeropsio/zerops/0.1.0/$arch/terraform-provider-zerops_v0.1.0"
done
cat > "$root/.smoke/init.tfrc" <<EOF
provider_installation {
  filesystem_mirror {
    path = "$mirror"
    include = ["registry.terraform.io/zeropsio/zerops", "registry.opentofu.org/zeropsio/zerops"]
  }
}
EOF
for cli in /usr/bin/terraform /usr/bin/tofu; do
  [[ -x "$cli" ]] || { echo "Missing CLI: $cli" >&2; exit 1; }
  (
    work="$root/.smoke/$(basename "$cli")"
    mkdir -p "$work"
    # Keep previous state on retries; never overwrite or discard a state binding.
    cp -R examples/smoke/. "$work/"
    cd "$work"
    project_id=""
    binding_removed=false
    address="${SMOKE_IMPORT_ADDRESS:-zerops_project.smoke}"
    cleanup() {
      status=$?
      trap - EXIT
      if [[ "$binding_removed" == true ]]; then
        if ! "$cli" import -input=false "$address" "$project_id"; then
          echo "Cannot restore removed state binding; project $project_id requires manual recovery" >&2
          exit 1
        fi
      fi
      if ! "$cli" destroy -auto-approve -input=false; then
        echo "Smoke cleanup FAILED for $cli; retain $work state and process diagnostics above" >&2
        exit 1
      fi
      if [[ -n "$project_id" ]]; then
        [[ "$project_id" =~ ^[A-Za-z0-9_-]+$ ]] || exit 1
        if [[ "${ZEROPS_TARGET:-}" == prod ]]; then
          # Token stays in Go's environment, never in a curl argument.
          result="$("$root/bin/zguard" request GET "/project/$project_id")" || exit 1
          code="$(jq -r .status <<<"$result")"
          jq -r .body <<<"$result" > "$work/deleted-project.json"
        else
          # Keep the bearer in stdin, not process arguments; redirects stay disabled.
          code="$(printf 'header = "Authorization: Bearer %s"\n' "$ZEROPS_TOKEN" | curl --config - --silent --show-error --max-time 60 --proto '=https' -o "$work/deleted-project.json" -w '%{http_code}' "${ZEROPS_ENDPOINT%/}/api/rest/public/project/$project_id")" || exit 1
        fi
        if [[ "$code" != 400 ]] || ! jq -e '.error.code == "projectNotFound"' "$work/deleted-project.json" >/dev/null; then
          echo "Project absence not confirmed for $project_id" >&2; exit 1
        fi
      fi
      exit "$status"
    }
    trap cleanup EXIT
    # This generated fixture reuses 0.1.0 after local builds. Regenerate its
    # checksums and installed package from the current mirror, not an old build.
    # Keep all state/backups and .terraform backend/workspace metadata intact.
    rm -f -- .terraform.lock.hcl
    for host in "${hosts[@]}"; do rm -rf -- ".terraform/providers/$host/zeropsio/zerops/0.1.0"; done
    TF_CLI_CONFIG_FILE="$root/.smoke/init.tfrc" "$cli" init -input=false
    "$cli" plan -input=false -out=smoke.tfplan
    "$cli" apply -input=false smoke.tfplan
    project_id="$("$cli" output -raw project_id)"
    "$cli" plan -input=false -detailed-exitcode
    "$cli" state rm "$address"
    binding_removed=true
    "$cli" import -input=false "$address" "$project_id"
    binding_removed=false
    "$cli" plan -input=false -detailed-exitcode
  )
done
