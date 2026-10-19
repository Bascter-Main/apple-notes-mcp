#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
repo="third_party/icloud-notes"
commit="f5b8dcc3e2e5c9d7450472265fc15b5c763891f2"

if ! command -v go >/dev/null; then
  echo 'Go is required. Install Go 1.26+ from https://go.dev/dl/ first.' >&2
  exit 1
fi
if ! command -v git >/dev/null || ! command -v python3 >/dev/null; then
  echo 'Requires git and python3.' >&2
  exit 1
fi
if [ ! -d "$repo/.git" ]; then
  mkdir -p third_party
  git clone -q https://github.com/torkve/icloud-notes.git "$repo"
fi
actual="$(git -C "$repo" rev-parse HEAD)"
if [ "$actual" != "$commit" ]; then
  git -C "$repo" fetch -q origin "$commit"
  git -C "$repo" checkout --detach -q "$commit"
fi
# The checkout must be pristine before patching; no partial modifications.
if [ -n "$(git -C "$repo" status --porcelain)" ]; then
  echo 'Upstream checkout has uncommitted changes. Reset it manually only after inspection.' >&2
  exit 1
fi
python3 scripts/prepare_upstream.py
export GOTOOLCHAIN=auto
# The original library requires Go 1.26; auto can fetch the newer toolchain.
go mod edit -replace=github.com/torkve/icloud-notes=./third_party/icloud-notes
go get github.com/torkve/icloud-notes@v1.0.0
go mod tidy
go mod verify
go test -tags icloud ./...
go vet -tags icloud ./...
mkdir -p bin
go build -tags icloud -trimpath -o bin/notes-mcp ./cmd/notes-mcp
go build -tags icloud -trimpath -o bin/notesctl ./cmd/notesctl
printf '\nCompiled successfully: bin/notes-mcp and bin/notesctl\n'
