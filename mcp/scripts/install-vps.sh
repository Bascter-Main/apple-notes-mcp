#!/usr/bin/env bash
# Run on a Linux VPS AFTER scripts/bootstrap.sh compiled the two binaries.
set -euo pipefail
cd "$(dirname "$0")/.."
if [ "$(id -u)" != 0 ]; then echo 'Please run with sudo: sudo bash scripts/install-vps.sh' >&2; exit 1; fi
if [ ! -x bin/notes-mcp ] || [ ! -x bin/notesctl ]; then
  echo 'Compile binaries first: bash scripts/bootstrap.sh' >&2; exit 1
fi
if ! id notesmcp >/dev/null 2>&1; then
  useradd --system --no-create-home --shell /usr/sbin/nologin notesmcp
fi
install -d -m 0755 /opt/icloud-notes-mcp/bin
install -m 0755 bin/notes-mcp /opt/icloud-notes-mcp/bin/notes-mcp
install -m 0755 bin/notesctl /opt/icloud-notes-mcp/bin/notesctl
install -d -o notesmcp -g notesmcp -m 0700 /var/lib/notes-mcp
install -d -o root -g root -m 0700 /etc/notes-mcp
if [ ! -e /etc/notes-mcp/config.env ]; then
  install -o root -g root -m 0600 deploy/vps/config.env.example /etc/notes-mcp/config.env
fi
install -o root -g root -m 0644 deploy/vps/notes-mcp.service /etc/systemd/system/notes-mcp.service
systemctl daemon-reload
cat <<'EOM'
Installed but NOT started.
1) sudo -u notesmcp /opt/icloud-notes-mcp/bin/notesctl -action login -state-dir /var/lib/notes-mcp
2) sudo -u notesmcp /opt/icloud-notes-mcp/bin/notesctl -action folders -state-dir /var/lib/notes-mcp
3) sudoedit /etc/notes-mcp/config.env  # set ALLOWED_FOLDER_IDS to an iCloud folder ID
4) sudo systemctl enable --now notes-mcp
5) curl http://127.0.0.1:8765/healthz
Do not open 8765 in your firewall. The MCP endpoint is designed for a private tunnel.
EOM
