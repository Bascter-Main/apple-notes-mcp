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

issuer="https://${PUBLIC_HOST}"
conf=/etc/nginx/conf.d/notes-mcp.conf
[ -f "$conf" ] || { echo "Missing Nginx config" >&2; exit 1; }
[ -r /etc/notes-gateway/gateway.env ] || { echo "Missing gateway allowlist" >&2; exit 1; }
systemctl is-active --quiet authplane
systemctl is-active --quiet notes-mcp
systemctl is-active --quiet notes-gateway
[ "$(curl -fsS http://127.0.0.1:8765/healthz)" = alive ] || exit 1
[ "$(curl -sS -o /dev/null -w '%{http_code}' --max-time 10 \
  -X POST http://127.0.0.1:8766/mcp -H 'Content-Type: application/json' -d '{}')" = 401 ] \
  || { echo "Gateway not rejecting unauthenticated requests" >&2; exit 1; }
[ "$(curl -sS -o /dev/null -w '%{http_code}' --max-time 10 \
  --resolve "${PUBLIC_HOST}:443:127.0.0.1" -X POST "${issuer}/mcp" \
  -H 'Content-Type: application/json' -d '{}')" = 403 ] \
  || { echo "Public /mcp is not at the safe 403 baseline" >&2; exit 1; }

stamp=$(date +%Y%m%dT%H%M%S)
backup_dir="/root/notes-mcp-backups/oauth-activate-${stamp}"
install -d -m 0700 "$backup_dir"
cp -a "$conf" "$backup_dir/notes-mcp.conf"
echo "Backup: $backup_dir"

restore() {
  echo "Restoring Nginx to the previous public /mcp = 403 configuration" >&2
  cp -a "$backup_dir/notes-mcp.conf" "$conf"
  nginx -t && systemctl reload nginx || true
}
trap restore ERR

python3 - "$conf" <<'PY'
from pathlib import Path
import sys

p = Path(sys.argv[1])
text = p.read_text(encoding="utf-8")
old = '''    # Unauthenticated note access remains impossible at this stage.
    location = /mcp {
        return 403;
    }
'''
new = '''    # OAuth JWT, resource/audience/scope, and owner subject are enforced
    # by the localhost gateway BEFORE requests reach notes-mcp :8765.
    location = /mcp {
        proxy_pass http://127.0.0.1:8766;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto https;
        proxy_set_header Authorization $http_authorization;
        proxy_set_header Connection "";
        proxy_buffering off;
        proxy_read_timeout 200s;
        client_max_body_size 2m;
    }
'''
if text.count(old) != 1:
    raise SystemExit("Required fail-closed block was not found exactly once; Nginx unchanged")
p.write_text(text.replace(old, new), encoding="utf-8")
PY
nginx -t
systemctl reload nginx

# A just-reloaded Nginx may briefly hand a connection to an old worker. Retry
# for at most ten one-second intervals before treating activation as failed.
headers=
for _ in {1..10}; do
  headers=$(curl -sS -D - -o /dev/null --max-time 10 \
    --resolve "${PUBLIC_HOST}:443:127.0.0.1" -X POST "${issuer}/mcp" \
    -H 'Content-Type: application/json' -d '{}' || true)
  if grep -Eq '^HTTP/[0-9.]+ 401' <<<"$headers" \
    && grep -iq '^WWW-Authenticate: Bearer' <<<"$headers"; then
    break
  fi
  sleep 1
done
if ! grep -Eq '^HTTP/[0-9.]+ 401' <<<"$headers"; then
  echo "Public gateway did not return 401 for a no-token request" >&2
  false
fi
if ! grep -iq '^WWW-Authenticate: Bearer' <<<"$headers"; then
  echo "Missing OAuth WWW-Authenticate challenge" >&2
  false
fi
trap - ERR
echo "Protected MCP activated: unauthenticated HTTPS /mcp returns HTTP 401 with WWW-Authenticate."
echo "Valid owner-scoped OAuth tokens can now be forwarded to the private notes-mcp."
