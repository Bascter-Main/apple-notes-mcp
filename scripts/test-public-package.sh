#!/usr/bin/env bash
set -euo pipefail

root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
for cmd in bash go git python3; do
  command -v "$cmd" >/dev/null || { echo "Missing prerequisite: $cmd" >&2; exit 1; }
done

for script in "$root"/scripts/*.sh "$root"/mcp/scripts/*.sh; do
  bash -n "$script"
done
python3 - "$root/scripts/01-provision.py" "$root/mcp/scripts/prepare_upstream.py" <<'PY'
from pathlib import Path
import sys

for name in sys.argv[1:]:
    compile(Path(name).read_text(encoding="utf-8"), name, "exec")
PY
python3 - "$root/deploy/nginx/notes-mcp-blocked.conf.template" \
  "$root/deploy/nginx/notes-mcp-protected.conf.template" <<'PY'
from pathlib import Path
import sys

blocked = Path(sys.argv[1]).read_text(encoding="utf-8")
protected = Path(sys.argv[2]).read_text(encoding="utf-8")
old = '''    # Unauthenticated note access remains impossible at this stage.
    location = /mcp {
        return 403;
    }
'''
new = '''    # OAuth JWT, resource/audience/scope, and owner subject are enforced
    # by the localhost gateway BEFORE requests reach notes-mcp :8765.
    location = /mcp {
        proxy_pass http://127.0.0.1:8766;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto https;
        proxy_set_header Authorization $http_authorization;
        proxy_set_header Connection "";
        proxy_buffering off;
        proxy_read_timeout 200s;
        client_max_body_size 2m;
    }
'''
if blocked.count(old) != 1 or blocked.replace(old, new) != protected:
    raise SystemExit("Nginx blocked-to-protected transition is inconsistent")
PY

gofmt_diff=$(find "$root/gateway" "$root/mcp/cmd" "$root/mcp/internal" \
  -type f -name '*.go' -print0 | xargs -0 gofmt -d)
if [ -n "$gofmt_diff" ]; then
  printf '%s\n' "$gofmt_diff" >&2
  echo "Go source is not gofmt-clean" >&2
  exit 1
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
(
  cd "$root/gateway"
  go mod download
  go mod verify
  go test ./...
  go vet ./...
  go build -trimpath -o "$work/notes-gateway" .
)
cp -a "$root/mcp" "$work/mcp"
(
  cd "$work/mcp"
  bash scripts/bootstrap.sh
)

test -x "$work/notes-gateway"
test -x "$work/mcp/bin/notes-mcp"
test -x "$work/mcp/bin/notesctl"
echo "Public package build and tests passed."
