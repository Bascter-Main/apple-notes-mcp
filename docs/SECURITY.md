# Security notes

## Never commit

- Apple ID, password, 2FA code, session cookies, `session.key`, or
  `session.enc`.
- `/etc/notes-mcp/config.env`, `/etc/notes-gateway/gateway.env`, or
  `/etc/authplane/secrets.env` from a real server.
- Authplane SQLite data, signing keys, access/refresh tokens, TLS private keys,
  logs, note titles, note bodies, attachments, or exported user data.

The example files contain placeholders only. `.gitignore` is defense in depth,
not permission to store secrets in the repository.

## Network boundaries

- `notes-mcp`: `127.0.0.1:8765` only.
- `notes-gateway`: `127.0.0.1:8766` only.
- Authplane OAuth: `127.0.0.1:9000` only.
- Authplane admin: `127.0.0.1:9001` only and never proxied by Nginx.
- Nginx proxies exact `/mcp` only to 8766, never to 8765.

## Authorization boundaries

The gateway validates token signature, issuer, audience/resource, expiry,
`notes:read`, and a single allowed `sub` before forwarding. It removes bearer,
cookie, proxy-authorization, and inbound forwarded headers. The core MCP then
independently limits Notes to configured folder roots and all descendants.

## Reporting

Share sanitized status only. Do not include request dumps from Apple/Authplane
or real note metadata in public issues. Rotate affected credentials and remove
published material from history if a secret is ever committed.
