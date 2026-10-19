# Third-party notices

This repository does not redistribute the Authplane server binary or its
runtime database. Deployment downloads Authplane `v0.2.3` from its official
GitHub release and verifies the release checksum. Authplane server v0.2.3 is
licensed under GNU AGPL v3; its license text is preserved in
`licenses/authplane-authserver-AGPL-3.0.txt`. This project uses the unmodified
upstream binary as a separate service.

The build uses these pinned dependencies:

- `github.com/torkve/icloud-notes`, pinned to commit
  `f5b8dcc3e2e5c9d7450472265fc15b5c763891f2`, MIT License. The build applies
  the narrowly scoped compatibility and read-safety changes in
  `mcp/scripts/prepare_upstream.py`.
- `github.com/authplane/go-sdk/core v0.4.0`, Apache License 2.0.
- `github.com/authplane/go-sdk/http v0.4.0`, Apache License 2.0.
- `github.com/go-jose/go-jose/v4 v4.1.4`, Apache License 2.0.
- `github.com/google/uuid v1.6.0`, BSD 3-Clause License.
- `google.golang.org/protobuf v1.36.11`, BSD 3-Clause License.

The corresponding license texts are preserved in `licenses/`. Transitive
module versions and checksums remain locked in `mcp/go.sum` and
`gateway/go.sum`.
