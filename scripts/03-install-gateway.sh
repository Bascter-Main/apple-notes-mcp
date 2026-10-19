#!/usr/bin/env bash
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
  echo "Must run as root" >&2
  exit 1
fi
for cmd in go curl systemctl python3 ss; do
  command -v "$cmd" >/dev/null || { echo "Missing prerequisite: $cmd" >&2; exit 1; }
done
: "${PUBLIC_HOST:?Set PUBLIC_HOST to the HTTPS hostname, for example mcp.example.com}"
if [[ ! "$PUBLIC_HOST" =~ ^[A-Za-z0-9][A-Za-z0-9.-]*[A-Za-z0-9]$ ]] || [[ "$PUBLIC_HOST" == *..* ]]; then
  echo "PUBLIC_HOST is not a valid hostname" >&2
  exit 1
fi

base=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
issuer="https://${PUBLIC_HOST}"
[ -r /etc/notes-gateway/gateway.env ] || { echo "Missing gateway.env; run 01-provision.py" >&2; exit 1; }
curl -fsS --max-time 12 "${issuer}/.well-known/oauth-authorization-server" \
  | python3 -c 'import json,sys; expected=sys.argv[1]; assert json.load(sys.stdin)["issuer"] == expected' "$issuer"
curl -fsS --max-time 12 "${issuer}/.well-known/jwks.json" \
  | python3 -c 'import json,sys; assert len(json.load(sys.stdin).get("keys", [])) > 0'
if ss -lntH '( sport = :8766 )' | grep -q .; then
  echo "Port 8766 already in use; aborting" >&2
  exit 1
fi
if [ -f /etc/systemd/system/notes-gateway.service ]; then
  echo "Gateway service already exists; aborting to avoid overwriting it" >&2
  exit 1
fi

build_dir=$(mktemp -d)
trap 'rm -rf "$build_dir"' EXIT
(
  cd "$base/gateway"
  go mod download
  go mod verify
  go test ./...
  go vet ./...
  go build -trimpath -o "$build_dir/notes-gateway" .
)
[ -x "$build_dir/notes-gateway" ] || exit 1

if ! id notesgateway >/dev/null 2>&1; then
  useradd --system --no-create-home --shell /usr/sbin/nologin notesgateway
fi
install -m 0755 "$build_dir/notes-gateway" /usr/local/bin/notes-gateway
install -m 0644 "$base/deploy/systemd/notes-gateway.service" /etc/systemd/system/notes-gateway.service
systemctl daemon-reload
systemctl enable --now notes-gateway.service
sleep 3
if ! systemctl is-active --quiet notes-gateway.service; then
  echo "Gateway startup failed; inspect journalctl -u notes-gateway -n 60" >&2
  exit 1
fi
code=$(curl -sS -o /dev/null -w '%{http_code}' --max-time 10 \
  -X POST http://127.0.0.1:8766/mcp -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}')
[ "$code" = 401 ] || { echo "Expected local unauthenticated 401, got $code" >&2; exit 1; }
curl -fsS --max-time 10 http://127.0.0.1:8766/.well-known/oauth-protected-resource/mcp \
  | python3 -c 'import json,sys; expected=sys.argv[1]; d=json.load(sys.stdin); assert d["resource"] == expected; assert "notes:read" in d["scopes_supported"]' "${issuer}/mcp"
echo "Gateway active; local /mcp returns 401 without a token. Public /mcp remains 403."
