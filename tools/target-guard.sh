# shellcheck shell=bash
# Source this file; never print credentials. Keep tracing disabled for credentials.
zerops_target_precheck() {
  set +x
  if [[ -n "${ZEROPS_ACC_ENV_FILE:-}" ]]; then
    set -a
    source "$ZEROPS_ACC_ENV_FILE" || { set +a; return 1; }
    set +a
  fi
  local host domain token="${ZEROPS_TOKEN:-}" org="${ZEROPS_ORGANIZATION_ID:-}"
  if [[ -z "${token//[[:space:]]/}" || -z "${org//[[:space:]]/}" ]]; then
    echo 'ZEROPS_TOKEN and ZEROPS_ORGANIZATION_ID are required' >&2; return 1
  fi
  case "${ZEROPS_TARGET:-dev}" in
    ''|dev)
      domain="${ZEROPS_DEV_DOMAIN:-}"
      if [[ -z "$domain" ]]; then echo 'ZEROPS_DEV_DOMAIN is required' >&2; return 1; fi
      domain="${domain,,}"
      local label
      if [[ ${#domain} -gt 253 || "$domain" == *..* || "$domain" == .* || "$domain" == *. || "$domain" =~ ^[0-9.]+$ ]]; then
        echo 'ZEROPS_DEV_DOMAIN must be a DNS domain suffix' >&2; return 1
      fi
      local -a labels
      IFS=. read -r -a labels <<< "$domain"
      for label in "${labels[@]}"; do
        if [[ ${#label} -gt 63 || ! "$label" =~ ^[a-z0-9]([a-z0-9-]*[a-z0-9])?$ ]]; then
          echo 'ZEROPS_DEV_DOMAIN must be a DNS domain suffix' >&2; return 1
        fi
      done
      if [[ ! "${ZEROPS_ENDPOINT:-}" =~ ^https://([a-zA-Z0-9.-]+)(:[0-9]+)?$ ]]; then
        echo 'ZEROPS_ENDPOINT is required and must be an HTTPS origin without a path, credentials, or query' >&2; return 1
      fi
      host="${BASH_REMATCH[1],,}"
      if [[ "$host" == api.app-prg1.zerops.io || ( "$host" != "$domain" && "$host" != *."$domain" ) ]]; then
        echo 'ZEROPS_ENDPOINT must be inside ZEROPS_DEV_DOMAIN and must not be production' >&2; return 1
      fi
      export ZEROPS_SANDBOX=dev
      ;;
    prod)
      if [[ "${ZEROPS_ENDPOINT:-}" != https://api.app-prg1.zerops.io ]]; then
        echo 'Production requires ZEROPS_ENDPOINT=https://api.app-prg1.zerops.io' >&2; return 1
      fi
      if [[ "${ZEROPS_PROD_LEDGER:-}" != /* ]]; then
        echo 'Production requires an absolute ZEROPS_PROD_LEDGER path' >&2; return 1
      fi
      export ZEROPS_SANDBOX=prod
      local resolved
      resolved="$(bin/zguard resolve-org)" || return 1
      [[ "$resolved" == "$org" ]] || { echo 'Production organization membership mismatch' >&2; return 1; }
      ;;
    *) echo 'ZEROPS_TARGET must be empty, dev, or prod' >&2; return 1 ;;
  esac
}

# Hold this descriptor until all work and cleanup finish; never export it.
zerops_production_run_lock() {
  local root="${1:-$PWD}"
  exec {ZEROPS_RUN_FD}>>"${ZEROPS_RUN_LOCK:-$root/.acc-run.lock}" || return 1
  if ! flock -n "$ZEROPS_RUN_FD"; then
    echo 'Another acceptance, smoke, or sweep run holds the run lock; refusing overlap' >&2
    exec {ZEROPS_RUN_FD}>&-
    return 1
  fi
}
