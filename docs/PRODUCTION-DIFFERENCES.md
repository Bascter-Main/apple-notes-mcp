# Production-aligned changes

This public tree is based on the source that produced the running MCP and
gateway binaries. The following corrections were made during deployment and
are incorporated here:

1. Authplane release verification now selects the checksum line by exact asset
   filename. A prefix match also selected the SBOM checksum and caused a safe
   pre-install failure.
2. OAuth exposure waits up to ten one-second intervals for a newly reloaded
   Nginx worker before failing and restoring the previous configuration.
3. Protected MCP activation uses the same bounded retry for the expected 401
   plus Bearer challenge. This avoids mistaking a brief response from an old
   Nginx worker for a failed activation.
4. Authplane DCR uses `approved_redirects`, with only
   `https://chatgpt.com/connector_platform_oauth_redirect`, rather than open
   registration or the original `admin_only` first-stage setting.
5. The diagnostic `notesctl` actions `session-check` and `login-test` are
   retained. They disclose only session-shape booleans/counts or a sanitized
   live-read status, never credential or token values.
6. Production-specific hostnames and backup paths were replaced by the
   validated `PUBLIC_HOST` / `AUTHPLANE_ISSUER` settings and generic backup
   paths. The gateway still requires an HTTPS origin and an audience equal to
   that origin plus `/mcp`.
7. The public gateway installer uses `go mod download`, `go mod verify`, tests,
   vet, and a temporary build output instead of mutating dependency files with
   `go mod tidy` during installation.

No runtime configuration, Apple session, Authplane database, OAuth identity,
certificate, note data, log, or prebuilt binary is part of this tree.
