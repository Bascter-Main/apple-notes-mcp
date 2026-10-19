# VPS deployment

The steps below preserve a fail-closed sequence: the public `/mcp` endpoint is
HTTP 403 until the local OAuth gateway is installed and independently returns
401 with a Bearer challenge.

## 1. Prerequisites

- A Linux VPS with systemd, Nginx, Python 3, Git, curl, OpenSSL, and Go 1.26+.
- A DNS hostname and a valid certificate under
  `/etc/letsencrypt/live/<hostname>/`.
- Ports 8765, 8766, 9000, and 9001 must not be exposed by the firewall.

Copy `.env.example` to `.env`, replace `mcp.example.com`, then export it:

```bash
set -a
. ./.env
set +a
```

Never commit `.env`.

## 2. Build and install the private MCP core

```bash
cd mcp
bash scripts/bootstrap.sh
sudo bash scripts/install-vps.sh
```

The installer does not start the service. In your SSH terminal, sign in and
select a non-sensitive test folder:

```bash
sudo -u notesmcp /opt/apple-notes-mcp/bin/notesctl \
  -action login -state-dir /var/lib/notes-mcp
sudo -u notesmcp /opt/apple-notes-mcp/bin/notesctl \
  -action folders -state-dir /var/lib/notes-mcp
sudoedit /etc/notes-mcp/config.env
sudo systemctl enable --now notes-mcp
curl -fsS http://127.0.0.1:8765/healthz
```

The Apple password and 2FA code are terminal-only. Do not put them in an env
file. `ALLOWED_FOLDER_IDS` accepts opaque folder IDs; each configured root
includes its descendants. A missing root or a scan exceeding
`MAX_SCAN_NOTES` fails closed.

## 3. Install loopback-only Authplane

From the repository root:

```bash
sudo PUBLIC_HOST="$PUBLIC_HOST" bash scripts/install-authplane-vps-stage1.sh
```

The script downloads Authplane v0.2.3, verifies the exact release asset against
the official checksum list, creates a low-privilege user, generates independent
random secrets, and starts only loopback listeners on 9000 and 9001. It enables
CIMD and permits only ChatGPT's fixed connector OAuth redirect.

## 4. Provision one OAuth resource and owner

```bash
sudo AUTHPLANE_ISSUER="$AUTHPLANE_ISSUER" python3 scripts/01-provision.py
```

The prompted email and password are for the local OAuth account, not the Apple
account. The script creates a `notes:read` resource and writes only the selected
Authplane user ID to root-only `/etc/notes-gateway/gateway.env`.

## 5. Publish OAuth while keeping MCP blocked

```bash
sudo PUBLIC_HOST="$PUBLIC_HOST" bash scripts/02-expose-oauth-still-block-mcp.sh
```

Verify discovery is 200 and public `/mcp` remains 403. The script backs up any
existing site and rolls back if validation fails.

## 6. Build and install the local gateway

```bash
sudo PUBLIC_HOST="$PUBLIC_HOST" bash scripts/03-install-gateway.sh
```

Expected state: `notes-gateway` is active/enabled, 8766 listens only on
127.0.0.1, and no-token/invalid-token local requests both return 401. Public
`/mcp` is still 403.

## 7. Activate protected MCP

```bash
sudo PUBLIC_HOST="$PUBLIC_HOST" bash scripts/04-activate-protected-mcp.sh
```

The script backs up Nginx, changes only the exact fail-closed `/mcp` block, and
requires a public 401 plus `WWW-Authenticate: Bearer`. It retries for at most
ten seconds to tolerate Nginx old-worker handoff, then restores the 403 config
on failure.

## 8. Verify security boundaries

```bash
curl -sS -o /dev/null -w 'MCP no token: %{http_code}\n' \
  -X POST "https://${PUBLIC_HOST}/mcp" \
  -H 'Content-Type: application/json' -d '{}'
curl -sS -o /dev/null -w 'Discovery: %{http_code}\n' \
  "https://${PUBLIC_HOST}/.well-known/oauth-authorization-server"
curl -sS -o /dev/null -w 'Admin: %{http_code}\n' \
  "https://${PUBLIC_HOST}/admin/users"
sudo ss -lntp '( sport = :8765 or sport = :8766 or sport = :9000 or sport = :9001 )'
```

Expected results: 401, 200, 403, and four listeners bound only to 127.0.0.1.
Never route Nginx directly to port 8765.

## Rollback

Each Nginx-changing script prints a timestamped directory under
`/root/notes-mcp-backups/`. Restore the saved `notes-mcp.conf`, run `nginx -t`,
then reload Nginx. Do not bypass OAuth or broaden the Notes folder allowlist to
debug an authorization failure.
