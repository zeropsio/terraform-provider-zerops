#!/usr/bin/env bash
# Restricted DNS fixture helper. Never enable tracing around credential handling.
set +x
set -euo pipefail
umask 077
ZONE="${CLOUDFLARE_ZONE_ID:?CLOUDFLARE_ZONE_ID is required}"
DOMAIN="${ZEROPS_ACC_DNS_DOMAIN:?ZEROPS_ACC_DNS_DOMAIN is required}"
DOMAIN="${DOMAIN,,}"
[[ "$DOMAIN" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ && "$DOMAIN" != *..* ]] || { echo "Invalid ZEROPS_ACC_DNS_DOMAIN" >&2; exit 97; }
: "${ZEROPS_PROD_LEDGER:?ZEROPS_PROD_LEDGER is required}"
[[ "$ZEROPS_PROD_LEDGER" == /* ]] || { echo "ZEROPS_PROD_LEDGER must be absolute" >&2; exit 97; }
BASE="https://api.cloudflare.com/client/v4/zones/$ZONE/dns_records"
guard_name() {
  local name="${1,,}"
  if [[ "$name" == "$DOMAIN" || "$name" == *."$DOMAIN" ]]; then
    [[ "$name" =~ ^[a-z0-9_*.-]+$ && "$name" != *..* && "$name" != .* && "$name" != *. ]] && return 0
  fi
  printf 'REFUSING DNS name outside ZEROPS_ACC_DNS_DOMAIN: %s (no mutation API call made)\n' "$1" >&2
  return 97
}
command="${1:-}"
case "$command" in
  create) [[ $# == 4 ]] || { echo 'Usage: cloudflare-dns.sh create TYPE NAME CONTENT' >&2; exit 2; }; guard_name "$3" ;;
  delete) [[ $# == 2 && "$2" =~ ^[a-fA-F0-9]{32}$ ]] || { echo 'Usage: cloudflare-dns.sh delete RECORD_ID (32 hex characters)' >&2; exit 2; } ;;
  list) [[ $# == 1 ]] || exit 2 ;;
  *) echo 'Usage: cloudflare-dns.sh create TYPE NAME CONTENT | delete RECORD_ID | list' >&2; exit 2 ;;
esac
if [[ -n "${CLOUDFLARE_API_TOKEN_FILE:-}" ]]; then
 [[ -f "$CLOUDFLARE_API_TOKEN_FILE" && ! -L "$CLOUDFLARE_API_TOKEN_FILE" && $(stat -c %a "$CLOUDFLARE_API_TOKEN_FILE") == 600 ]] || { echo "Cloudflare token file must have mode 600" >&2; exit 97; }
 CLOUDFLARE_API_TOKEN="$(<"$CLOUDFLARE_API_TOKEN_FILE")"
fi
: "${CLOUDFLARE_API_TOKEN:?CLOUDFLARE_API_TOKEN or CLOUDFLARE_API_TOKEN_FILE is required}"
header="$(mktemp)"
trap 'rm -f "$header"' EXIT
chmod 600 "$header"
printf 'Authorization: Bearer %s\n' "$CLOUDFLARE_API_TOKEN" > "$header"
api() {
  local method="$1" url="$2" body="${3:-}" result
  local args=(-q -sS --max-time 60 -X "$method" -H "@$header" -H 'Content-Type: application/json')
  [[ -z "$body" ]] || args+=(--data-binary "$body")
  result="$(curl "${args[@]}" "$url")"
  if ! jq -e '.success == true' >/dev/null <<<"$result"; then
    jq -c '{success,errors,messages}' <<<"$result" >&2
    return 1
  fi
  RESPONSE="$result"
}
ledger() {
  local kind="$1" id="$2" name="$3"
  if [[ ! -e "$ZEROPS_PROD_LEDGER" ]]; then (set -o noclobber; : > "$ZEROPS_PROD_LEDGER") 2>/dev/null || true; fi
  chmod 600 "$ZEROPS_PROD_LEDGER"
  printf '%s\t%s\t%s\t%s\tprobe\n' "$(date -u +%FT%TZ)" "$kind" "$id" "$name" >> "$ZEROPS_PROD_LEDGER"
}
case "$command" in
  create)
    body="$(jq -nc --arg type "${2^^}" --arg name "${3,,}" --arg content "$4" '{type:$type,name:$name,content:$content,ttl:60,proxied:false}')"
    api POST "$BASE" "$body"
    id="$(jq -er '.result.id' <<<"$RESPONSE")"
    name="$(jq -er '.result.name' <<<"$RESPONSE")"
    ledger dns "$id" "$name"
    jq -c '.result | {id,type,name,content,ttl,proxied}' <<<"$RESPONSE"
    ;;
  delete)
    api GET "$BASE/$2"
    guard_name "$(jq -er '.result.name' <<<"$RESPONSE")"
    api DELETE "$BASE/$2"
    ledger note "$2" dns-deleted
    jq -c '.result | {id}' <<<"$RESPONSE"
    ;;
  list)
    page=1
    while :; do
      api GET "$BASE?per_page=100&page=$page"
      jq -c --arg domain "$DOMAIN" '.result[] | select((.name|ascii_downcase) == $domain or (.name|ascii_downcase|endswith("."+$domain))) | {id,type,name,content,ttl,proxied}' <<<"$RESPONSE"
      (( page >= $(jq -r '.result_info.total_pages // 1' <<<"$RESPONSE") )) && break
      page=$((page+1))
    done
    ;;
esac
