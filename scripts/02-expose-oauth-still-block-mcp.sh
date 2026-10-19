#!/usr/bin/env bash
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
  echo "Must run as root" >&2
  exit 1
fi
for cmd in curl nginx systemctl python3; do
  command -v "$cmd" >/dev/null || { echo "Missing prerequisite: $cmd" >&2; exit 1; }
done
: "${PUBLIC_HOST:?Set PUBLIC_HOST to the HTTPS hostname, for example mcp.example.com}"
if [[ ! "$PUBLIC_HOST" =~ ^[A-Za-z0-9][A-Za-z0-9.-]*[A-Za-z0-9]$ ]] || [[ "$PUBLIC_HOST" == *..* ]]; then
  echo "PUBLIC_HOST is not a valid hostname" >&2
  exit 1
fi

base=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
issuer="https://${PUBLIC_HOST}"
config=/etc/nginx/conf.d/notes-mcp.conf
template="$base/deploy/nginx/notes-mcp-blocked.conf.template"
[ -f "$template" ] || { echo "Missing Nginx template" >&2; exit 1; }
[ -f "/etc/letsencrypt/live/${PUBLIC_HOST}/fullchain.pem" ] || { echo "TLS certificate not found" >&2; exit 1; }
[ -f /etc/notes-gateway/gateway.env ] || { echo "Run 01-provision.py first" >&2; exit 1; }
[ "$(curl -fsS --max-time 6 http://127.0.0.1:8765/healthz)" = alive ] || { echo "notes-mcp unhealthy" >&2; exit 1; }
curl -fsS --max-time 8 http://127.0.0.1:9000/.well-known/oauth-authorization-server \
  | python3 -c 'import json,sys; expected=sys.argv[1]; assert json.load(sys.stdin)["issuer"] == expected' "$issuer"
curl -fsS --max-time 8 http://127.0.0.1:9000/.well-known/oauth-protected-resource/mcp \
  | python3 -c 'import json,sys; expected=sys.argv[1]; assert json.load(sys.stdin)["resource"] == expected' "${issuer}/mcp"

stamp=$(date +%Y%m%dT%H%M%S)
backup_dir="/root/notes-mcp-backups/oauth-expose-${stamp}"
install -d -m 0700 "$backup_dir"
if [ -f "$config" ]; then
  cp -a "$config" "$backup_dir/notes-mcp.conf"
else
  : >"$backup_dir/no-previous-nginx-config"
fi
echo "Backup: $backup_dir"

restore() {
  echo "Restoring previous Nginx configuration" >&2
  if [ -f "$backup_dir/notes-mcp.conf" ]; then
    cp -a "$backup_dir/notes-mcp.conf" "$config"
  else
    mv "$config" "$backup_dir/failed-notes-mcp.conf"
  fi
  nginx -t && systemctl reload nginx || true
}
trap restore ERR

python3 - "$template" "$config" "$PUBLIC_HOST" <<'PY'
from pathlib import Path
import sys

template, destination, hostname = map(Path, sys.argv[1:])
text = template.read_text(encoding="utf-8")
if "__PUBLIC_HOST__" not in text:
    raise SystemExit("Nginx template placeholder missing")
rendered = text.replace("__PUBLIC_HOST__", str(hostname))
if "__PUBLIC_HOST__" in rendered:
    raise SystemExit("Nginx template rendering incomplete")
destination.write_text(rendered, encoding="utf-8")
PY
chmod 0644 "$config"

nginx -t
systemctl reload nginx
metadata=
for _ in {1..10}; do
  if metadata=$(curl -fsS --max-time 10 --resolve "${PUBLIC_HOST}:443:127.0.0.1" \
    "${issuer}/.well-known/oauth-authorization-server"); then
    break
  fi
  sleep 1
done
[ -n "$metadata" ] || { echo "OAuth discovery did not become ready after Nginx reload" >&2; false; }
printf '%s' "$metadata" | python3 -c 'import json,sys; expected=sys.argv[1]; d=json.load(sys.stdin); assert d["issuer"] == expected; assert "S256" in d["code_challenge_methods_supported"]; assert d.get("client_id_metadata_document_supported") is True; print("OAuth discovery: OK")' "$issuer"
code=$(curl -sS --max-time 10 -o /dev/null -w '%{http_code}' \
  --resolve "${PUBLIC_HOST}:443:127.0.0.1" -X POST "${issuer}/mcp" \
  -H 'Content-Type: application/json' -d '{}')
[ "$code" = 403 ] || { echo "Unexpected /mcp status: $code" >&2; false; }
trap - ERR
echo "OAuth routes online. MCP is still HTTP 403. Backup is at $backup_dir"
