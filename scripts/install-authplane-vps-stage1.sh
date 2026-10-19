#!/usr/bin/env bash
# Installs Authplane as a loopback-only OAuth server. It does not change Nginx
# or expose the MCP endpoint.
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
  echo "Please run as root" >&2
  exit 1
fi
for cmd in curl sha256sum tar openssl systemctl ss python3; do
  command -v "$cmd" >/dev/null || { echo "Missing prerequisite: $cmd" >&2; exit 1; }
done
: "${PUBLIC_HOST:?Set PUBLIC_HOST to the HTTPS hostname, for example mcp.example.com}"
if [[ ! "$PUBLIC_HOST" =~ ^[A-Za-z0-9][A-Za-z0-9.-]*[A-Za-z0-9]$ ]] || [[ "$PUBLIC_HOST" == *..* ]]; then
  echo "PUBLIC_HOST is not a valid hostname" >&2
  exit 1
fi

base=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
issuer="https://${PUBLIC_HOST}"
template="$base/deploy/authplane/config.yaml.example"
[ -f "$template" ] || { echo "Missing Authplane configuration template" >&2; exit 1; }
if [ -e /etc/authplane/config.yaml ] || [ -e /etc/systemd/system/authplane.service ]; then
  echo "Authplane config or service already exists; refusing to overwrite it" >&2
  exit 1
fi
if ss -lntH '( sport = :9000 or sport = :9001 )' | grep -q .; then
  echo "TCP port 9000 or 9001 is already in use" >&2
  exit 1
fi
if [ ! -f "/etc/letsencrypt/live/${PUBLIC_HOST}/fullchain.pem" ]; then
  echo "Existing TLS certificate not found" >&2
  exit 1
fi
if ! curl -fsS --max-time 5 http://127.0.0.1:8765/healthz | grep -qx 'alive'; then
  echo "Existing local MCP health check failed" >&2
  exit 1
fi

case "$(uname -m)" in
  x86_64) arch=amd64 ;;
  aarch64) arch=arm64 ;;
  *) echo "Unsupported CPU architecture: $(uname -m)" >&2; exit 1 ;;
esac
version=0.2.3
asset="authserver_${version}_linux_${arch}.tar.gz"
base_url="https://github.com/AuthPlane/authserver/releases/download/v${version}"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
curl -fLsS --retry 3 "${base_url}/${asset}" -o "${tmp}/${asset}"
curl -fLsS --retry 3 "${base_url}/checksums.txt" -o "${tmp}/checksums.txt"
(
  cd "$tmp"
  awk -v wanted="$asset" '$2 == wanted { print }' checksums.txt | sha256sum -c -
)
mkdir -p "${tmp}/extract"
tar -xzf "${tmp}/${asset}" -C "${tmp}/extract"
binary=$(find "${tmp}/extract" -type f -name authserver -print -quit)
[ -n "$binary" ] || { echo "Authplane binary not found in verified archive" >&2; exit 1; }
install -m 0755 "$binary" /usr/local/bin/authserver
/usr/local/bin/authserver version

if ! id authplane >/dev/null 2>&1; then
  useradd --system --no-create-home --shell /usr/sbin/nologin authplane
fi
install -d -o root -g authplane -m 0750 /etc/authplane
install -d -o authplane -g authplane -m 0700 /var/lib/authplane
install -d -o authplane -g authplane -m 0700 /var/lib/authplane/keys

python3 - "$template" /etc/authplane/config.yaml "$PUBLIC_HOST" <<'PY'
from pathlib import Path
import sys

template = Path(sys.argv[1])
destination = Path(sys.argv[2])
hostname = sys.argv[3]
text = template.read_text(encoding="utf-8")
if "__PUBLIC_HOST__" not in text:
    raise SystemExit("Authplane template placeholder missing")
rendered = text.replace("__PUBLIC_HOST__", hostname)
if "__PUBLIC_HOST__" in rendered:
    raise SystemExit("Authplane template rendering incomplete")
destination.write_text(rendered, encoding="utf-8")
PY
chown root:authplane /etc/authplane/config.yaml
chmod 0640 /etc/authplane/config.yaml

(
  umask 077
  printf 'AUTHPLANE_SESSION_SECRET=%s\n' "$(openssl rand -hex 32)" > /etc/authplane/secrets.env
  printf 'AUTHPLANE_ADMIN_API_KEY=%s\n' "$(openssl rand -hex 32)" >> /etc/authplane/secrets.env
  printf 'AUTHPLANE_DATA_ENCRYPTION_KEY=%s\n' "$(openssl rand -hex 32)" >> /etc/authplane/secrets.env
)
chown root:authplane /etc/authplane/secrets.env
chmod 0640 /etc/authplane/secrets.env

install -m 0644 "$base/deploy/systemd/authplane.service" /etc/systemd/system/authplane.service
systemctl daemon-reload
systemctl enable --now authplane.service
sleep 2
if ! systemctl is-active --quiet authplane.service; then
  echo "Authplane did not start; inspect journalctl -u authplane -n 50" >&2
  exit 1
fi
curl -fsS --max-time 10 http://127.0.0.1:9000/.well-known/oauth-authorization-server \
  | python3 -c 'import json,sys; expected=sys.argv[1]; d=json.load(sys.stdin); assert d.get("issuer") == expected; print("Issuer:", d.get("issuer")); print("PKCE methods:", d.get("code_challenge_methods_supported")); print("CIMD:", d.get("client_id_metadata_document_supported"))' "$issuer"
echo "Authplane is loopback-only. Nginx is unchanged; public /mcp must remain blocked."
