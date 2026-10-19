# Read-only iCloud Notes MCP with OAuth gateway

This repository packages the source and reproducible VPS deployment used for
a private, read-only iCloud Notes MCP. It is not an Apple SDK. The iCloud
adapter relies on undocumented CloudKit behavior and may break when Apple
changes its web services.

## Security model

```text
ChatGPT / MCP client
        |
        | HTTPS + OAuth bearer token
        v
Nginx :443
        |
        v
notes-gateway 127.0.0.1:8766
  - verifies JWT signature, issuer, resource audience and expiry
  - requires notes:read
  - requires one configured OAuth subject
  - removes Authorization, Cookie and forwarded headers
        |
        v
notes-mcp 127.0.0.1:8765
  - enforces folder-root allowlists recursively
  - exposes only read-only tools
  - never returns attachment URLs or image bytes
        |
        v
iCloud Notes / CloudKit

Authplane 127.0.0.1:9000 (OAuth) and :9001 (admin, never public)
```

The Apple session has account-level power even though the MCP tools are
read-only. Treat the VPS, `/var/lib/notes-mcp`, and the Authplane database as
sensitive. The core MCP, OAuth server, admin API, and gateway must remain on
loopback; only Nginx listens publicly.

## Repository layout

- `mcp/`: read-only MCP server, CLI, tests, pinned upstream patcher, and VPS
  installer.
- `gateway/`: OAuth resource-server gateway.
- `scripts/`: staged Authplane, Nginx, provisioning, gateway, and activation
  scripts.
- `deploy/`: systemd, Nginx, Authplane, and environment templates.
- `docs/DEPLOYMENT.md`: end-to-end VPS deployment and verification.
- `docs/PRODUCTION-DIFFERENCES.md`: fixes incorporated after the first
  deployment bundle.
- `licenses/` and `THIRD_PARTY_NOTICES.md`: dependency notices.

No Apple credentials, sessions, notes, OAuth users, tokens, private keys,
databases, logs, certificates, or production domain names belong in this
repository.

## Build and test

Prerequisites: Linux, Git, Python 3, Go 1.26 or newer, and outbound HTTPS.

Run the isolated validation script from the repository root:

```bash
bash scripts/test-public-package.sh
```

It copies the MCP source to a temporary directory, fetches the pinned
`torkve/icloud-notes` commit, applies deterministic compatibility patches,
runs all tagged Go tests, and builds both MCP binaries. It separately verifies,
tests, vets, and builds the gateway. Generated binaries and the fetched
upstream checkout do not enter the source tree.

For component-specific details, see `mcp/README.md` and
`docs/DEPLOYMENT.md`.

## Important limitations

- This is a single-owner design: the gateway permits one Authplane `sub`.
- Revocation may not invalidate an already issued access token until its short
  expiry; JWT validation is otherwise local after JWKS discovery.
- Apple ID, password, and 2FA are entered only in an SSH terminal. They are not
  OAuth credentials and must never be sent to ChatGPT.
- Start with a non-sensitive test folder. Expand the folder allowlist only
  after metadata and content reads have been verified.
- Do not weaken Apple account security merely to make an unofficial client
  work.

## License

Project code is released under the MIT License. Third-party components retain
their own licenses; see `THIRD_PARTY_NOTICES.md` and `licenses/`.
