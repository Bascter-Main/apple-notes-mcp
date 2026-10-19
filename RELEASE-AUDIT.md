# Release audit

Prepared on 2026-10-19 using Linux amd64 and Go 1.26.9.

- `scripts/test-public-package.sh`: passed.
- Gateway: `go mod verify`, `go test ./...`, `go vet ./...`, and a clean
  `-trimpath` build passed.
- MCP: the pinned upstream commit was fetched in an isolated directory; all
  deterministic patches applied; `go mod verify`, `go test -tags icloud
  ./...`, `go vet -tags icloud ./...`, and both `-trimpath` builds passed.
- Bash and Python syntax checks passed.
- Authplane YAML structure, all three systemd units, and both rendered Nginx
  states passed local syntax checks.
- Gitleaks v8.30.1 directory scan with full redaction: 0 findings.
- A separate manual scan found no private deployment literals, non-example
  email addresses, non-loopback IP addresses, private-key markers, suspicious
  runtime filenames, binary files, symlinks, or Git history.

The audit does not include real credentials or a live Apple/OAuth transaction.
Those are deployment-time checks and must never be copied into a public
artifact.
