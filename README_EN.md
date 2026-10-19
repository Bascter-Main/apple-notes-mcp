# Apple Notes MCP

[中文](README.md) | **English**

A read-only MCP service that securely connects Apple Notes to ChatGPT. It reads allowlisted Apple Notes data stored in iCloud on your own server and exposes MCP tools through HTTPS and OAuth, allowing GPT to retrieve folders, note metadata, and note text during a conversation.

This project is not an official Apple SDK. Its adapter relies on undocumented CloudKit endpoints and may require changes when Apple updates its services.

## Features and boundaries

- Read-only tools only; notes cannot be created, changed, or deleted.
- `ALLOWED_FOLDER_IDS` restricts access by folder ID and automatically includes descendant folders.
- Supports folder listing, note listing, time-based change queries, batched body reads, and sync status checks.
- Does not return attachment download URLs or image bytes.
- Apple ID, password, and two-factor codes are entered only in the VPS SSH terminal and are never sent to ChatGPT.
- Public requests pass through an OAuth gateway; the MCP core, OAuth service, and admin API listen on loopback only.
- The current deployment is single-user: the gateway authorizes one configured OAuth subject.

## Architecture

```text
ChatGPT
   │  HTTPS + OAuth access token
   ▼
Nginx :443
   ▼
notes-gateway 127.0.0.1:8766
   │  verifies signature, issuer, resource, expiry, notes:read, and subject
   ▼
notes-mcp 127.0.0.1:8765
   │  enforces the folder allowlist and read-only tools
   ▼
Apple Notes / iCloud CloudKit

Authplane 127.0.0.1:9000  OAuth service
Authplane 127.0.0.1:9001  admin API, never public
```

Only Nginx should listen publicly. Do not expose `8765`, `8766`, `9000`, or `9001`, and never proxy Nginx directly to `8765`.

## Deployment

The following procedure targets a systemd-based Linux VPS, preferably Debian or Ubuntu. The scripts use a staged, fail-closed sequence: public `/mcp` remains unavailable until the local OAuth gateway passes its rejection tests.

### 1. Prepare the server, domain, and TLS

You need:

- An `x86_64` or `arm64` Linux VPS;
- A hostname such as `mcp.example.com` pointing to the VPS;
- Nginx and a valid certificate under `/etc/letsencrypt/live/<hostname>/`;
- Git, Python 3, curl, OpenSSL, tar, unzip, and Go 1.26 or newer;
- Outbound access to iCloud, Apple authentication services, GitHub, and Go module hosts.

Install the base packages on Debian or Ubuntu:

```bash
sudo apt-get update
sudo apt-get install -y nginx git python3 curl openssl tar unzip ca-certificates
```

The Go toolchain must satisfy the repository requirement. If the distribution package is too old, install Go from [go.dev/dl](https://go.dev/dl/). Prepare the TLS certificate before continuing, for example with Certbot:

```bash
sudo apt-get install -y certbot python3-certbot-nginx
sudo certbot certonly --nginx -d mcp.example.com
```

The firewall only needs SSH, HTTP, and HTTPS. Do not expose `8765`, `8766`, `9000`, or `9001`.

### 2. Download the repository and configure the hostname

```bash
git clone https://github.com/Bascter-Main/apple-notes-mcp.git
cd apple-notes-mcp
cp .env.example .env
```

Edit `.env`:

```dotenv
PUBLIC_HOST=mcp.example.com
AUTHPLANE_ISSUER=https://mcp.example.com
```

Load it into the current shell:

```bash
set -a
. ./.env
set +a
```

`PUBLIC_HOST` is a hostname without `https://`; `AUTHPLANE_ISSUER` is the matching HTTPS origin. Never commit `.env`.

### 3. Build and install the MCP core

```bash
cd mcp
bash scripts/bootstrap.sh
sudo bash scripts/install-vps.sh
```

`bootstrap.sh` fetches the pinned `torkve/icloud-notes` source, applies this project's patches, runs tests, and builds `notes-mcp` and `notesctl`. The installer installs the binaries and systemd unit but does not start the MCP service yet.

### 4. Sign in to iCloud and configure the folder allowlist

Sign in from the SSH terminal:

```bash
sudo -u notesmcp /opt/apple-notes-mcp/bin/notesctl \
  -action login -state-dir /var/lib/notes-mcp
```

Enter the Apple ID, password, and six-digit verification code when prompted. Never place them in a configuration file, send them to ChatGPT, or commit them to GitHub.

List iCloud folder names and IDs:

```bash
sudo -u notesmcp /opt/apple-notes-mcp/bin/notesctl \
  -action folders -state-dir /var/lib/notes-mcp
```

Start with a non-sensitive test folder and authorize only that folder. Edit the service configuration:

```bash
sudoedit /etc/notes-mcp/config.env
```

At minimum, replace:

```dotenv
ALLOWED_FOLDER_IDS=YOUR_FOLDER_ID
```

Separate multiple root IDs with commas. Each root automatically includes all descendant folders. An empty allowlist, an unknown folder ID, or a scan exceeding `MAX_SCAN_NOTES` fails closed instead of broadening access.

Optionally count notes under one root without printing bodies:

```bash
sudo -u notesmcp /opt/apple-notes-mcp/bin/notesctl \
  -action probe -root 'FOLDER_ID' -state-dir /var/lib/notes-mcp
```

Start and check the MCP core:

```bash
sudo systemctl enable --now notes-mcp
sudo systemctl status notes-mcp --no-pager
curl -fsS http://127.0.0.1:8765/healthz
```

The expected response is `alive`.

### 5. Install the OAuth service

Return to the repository root and make sure the variables from step 2 are still loaded:

```bash
cd ..
sudo PUBLIC_HOST="$PUBLIC_HOST" bash scripts/install-authplane-vps-stage1.sh
```

The script downloads and verifies Authplane v0.2.3, generates independent random secrets, and binds the OAuth service and admin API to `127.0.0.1:9000` and `127.0.0.1:9001`. It does not modify Nginx or expose MCP.

### 6. Provision the OAuth resource and owner

```bash
sudo AUTHPLANE_ISSUER="$AUTHPLANE_ISSUER" \
  python3 scripts/01-provision.py
```

Create a sign-in email, display name, and OAuth password of at least 16 characters. This is a local OAuth account, not the Apple account. The script creates the `notes:read` resource and writes the selected user ID to the root-only gateway configuration.

### 7. Publish OAuth while keeping MCP blocked

```bash
sudo PUBLIC_HOST="$PUBLIC_HOST" \
  bash scripts/02-expose-oauth-still-block-mcp.sh
```

OAuth discovery should now return `200`, while public `/mcp` must remain `403`.

### 8. Install the OAuth gateway

```bash
sudo PUBLIC_HOST="$PUBLIC_HOST" bash scripts/03-install-gateway.sh
```

The script builds and starts `notes-gateway`, then checks that local requests with no token or an invalid token are rejected. Public `/mcp` remains `403`.

### 9. Activate the protected MCP endpoint

```bash
sudo PUBLIC_HOST="$PUBLIC_HOST" bash scripts/04-activate-protected-mcp.sh
```

The script replaces only the prevalidated Nginx `/mcp` deny block. After activation, a public request without a token must return `401` with `WWW-Authenticate: Bearer`. On failure, the script restores the previous `403` configuration.

### 10. Verify the deployment

```bash
curl -sS -o /dev/null -w 'MCP no token: %{http_code}\n' \
  -X POST "https://${PUBLIC_HOST}/mcp" \
  -H 'Content-Type: application/json' -d '{}'

curl -sS -o /dev/null -w 'OAuth discovery: %{http_code}\n' \
  "https://${PUBLIC_HOST}/.well-known/oauth-authorization-server"

curl -sS -o /dev/null -w 'Admin API: %{http_code}\n' \
  "https://${PUBLIC_HOST}/admin/users"

sudo ss -lntp \
  '( sport = :8765 or sport = :8766 or sport = :9000 or sport = :9001 )'
```

Expected results:

- `MCP no token: 401`
- `OAuth discovery: 200`
- `Admin API: 403`
- All four internal ports listen on `127.0.0.1` only

### 11. Connect ChatGPT

1. Open **Plugins** in ChatGPT on the web.
2. Select the plus button, then **Add custom MCP server**.
3. Enter a name such as `Apple Notes`.
4. Set the Server URL to `https://your-domain.example/mcp`.
5. Select **OAuth** authentication and use the server's advertised CIMD configuration.
6. Review the risk warning, create the plugin, and install it from your personal plugins.
7. Return to a conversation, type `@`, and select the plugin.
8. On the first tool call, sign in with the OAuth account created in step 6 and authorize `notes:read`.

Use the local OAuth account here, never the Apple ID or Apple password. See the [official OpenAI custom MCP guide](https://developers.openai.com/api/docs/guides/custom-mcp-server) for the current ChatGPT flow. Menu availability can vary by account plan and workspace policy.

After connecting, ask ChatGPT to call `get_sync_status` first, then use `list_folders`, `list_notes`, and `get_notes_batch` to verify the visible scope and text retrieval. During testing, avoid asking the model to repeat sensitive notes in its response.

## Configuration

The MCP core configuration is stored at `/etc/notes-mcp/config.env`:

| Variable | Default | Purpose |
| --- | --- | --- |
| `MCP_BIND` | `127.0.0.1:8765` | MCP core listen address; never make it public |
| `ICLOUD_STATE_DIR` | `/var/lib/notes-mcp` | Encrypted Apple session directory |
| `ALLOWED_FOLDER_IDS` | none | Allowlisted root folder IDs, comma-separated |
| `MAX_SCAN_NOTES` | `1000` | Safety limit for one scan |
| `ICLOUD_READ_TIMEOUT` | `90s` | iCloud read timeout |
| `MCP_CACHE_SECONDS` | `120` | In-memory cache lifetime; cleared on restart |

Restart the service after a change:

```bash
sudo systemctl restart notes-mcp
curl -fsS http://127.0.0.1:8765/healthz
```

## Updates and troubleshooting

Inspect service logs with:

```bash
sudo journalctl -u notes-mcp -n 100 --no-pager
sudo journalctl -u notes-gateway -n 100 --no-pager
sudo journalctl -u authplane -n 100 --no-pager
```

If the Apple session expires, run `notesctl -action login` again. Apple may rate-limit repeated sign-in attempts, so do not retry in a tight loop. Never publish Apple credentials, cookies, session files, OAuth tokens, real note titles, or note bodies in an issue or log excerpt.

Every script that modifies Nginx creates a timestamped backup under `/root/notes-mcp-backups/`. To roll back, restore the saved `notes-mcp.conf`, run `nginx -t`, and reload Nginx.

## Local package validation

From the repository root on Linux:

```bash
bash scripts/test-public-package.sh
```

The script validates the Shell and Python files, tests and builds the gateway, then fetches the pinned upstream source in a temporary directory, runs the tagged iCloud tests, and builds the MCP binaries.

## Known limitations

- The iCloud adapter depends on undocumented CloudKit behavior and is not covered by an Apple compatibility guarantee.
- The stored Apple session has account-level capability. Read-only behavior is enforced by this service and cannot eliminate the risk of a compromised VPS session.
- Only text is extracted; rich formatting, checklist state, attachments, and image OCR are not guaranteed to survive.
- Modification time is not the same as original authorship time, and overwritten historical versions are not retained.
- A previously issued short-lived access token may remain valid until it expires after account revocation.

See [`docs/SECURITY.md`](docs/SECURITY.md) for more security guidance.

## License

Project code is licensed under the [MIT License](LICENSE). Third-party components retain their own licenses; see [`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md).
