#!/usr/bin/env python3
"""Apply narrowly scoped and deterministic safety/compatibility fixes.

This script patches ONLY the pinned upstream version. It fails closed if any
expected code changes; it never falls back to incomplete timestamps or a
broadened folder scope.
"""
from pathlib import Path
import sys

BASE = Path(__file__).resolve().parents[1] / "third_party" / "icloud-notes"
UPSTREAM_COMMIT = "f5b8dcc3e2e5c9d7450472265fc15b5c763891f2"


def apply(path: str, before: str, after: str) -> None:
    p = BASE / path
    text = p.read_text(encoding="utf-8")
    if before not in text:
        if after in text:
            print(f"Already applied: {path}")
            return
        raise SystemExit(f"STOP: upstream file changed; refusing an unsafe partial patch: {path}")
    if text.count(before) != 1:
        raise SystemExit(f"STOP: ambiguous patch site: {path}")
    p.write_text(text.replace(before, after), encoding="utf-8")
    print(f"Patched: {path}")


def main() -> None:
    if not (BASE / "go.mod").exists():
        raise SystemExit("Missing third_party/icloud-notes checkout; run scripts/bootstrap.sh")

    # Add truthful CloudKit creation/modification timestamps to the domain Note.
    apply("notes/note.go",
          "\tTitle string\n\t// Body is the note's content.",
          "\tTitle string\n\t// CloudKit Unix milliseconds. Zero means unavailable.\n\tCreatedAtUnixMs int64\n\tModifiedAtUnixMs int64\n\tBodyDecoded bool\n\t// Body is the note's content.")
    apply("internal/cloudkit/schema.go",
          '"Deleted", "ModificationDate"}',
          '"Deleted", "CreationDate", "ModificationDate"}')
    apply("icloud/mapping.go",
          'Title:    r.DecodedField("TitleEncrypted", "title"),',
          'Title:    r.DecodedField("TitleEncrypted", "title"),\n\t\tCreatedAtUnixMs: r.IntField("CreationDate"),\n\t\tModifiedAtUnixMs: r.IntField("ModificationDate"),')
    apply("icloud/mapping.go",
          "\t\t\tn.Body = body\n\t\t\tseen := map[string]bool{}",
          "\t\t\tn.Body = body\n\t\t\tn.BodyDecoded = true\n\t\t\tseen := map[string]bool{}")

    # Upstream accidentally admits notes with an unresolved folder ID into a
    # subtree scan, and would resolve attachment URLs for those notes.
    apply("icloud/client.go",
          'if inScope[n.FolderID] || n.FolderID == "" {',
          'if n.FolderID != "" && inScope[n.FolderID] {')
    # We only return image counts, not download URLs. Avoid extra attachment
    # lookups and keep out-of-scope asset locators out of the data path.
    apply("icloud/client.go",
          "\tvar ns []notes.Note\n\tvar attIDs []string",
          "\tvar ns []notes.Note")
    apply("icloud/client.go",
          "\t\t\tns = append(ns, n)\n\t\t\tfor _, im := range n.Images {\n\t\t\t\tattIDs = append(attIDs, im.ID)\n\t\t\t}",
          "\t\t\tns = append(ns, n)")
    apply("icloud/client.go",
          "\tpages, pageURL := c.resolveAttachmentURLs(ctx, attIDs)\n\tfor i := range ns {\n\t\tapplyImageRefs(&ns[i], pages, pageURL)\n\t}\n",
          "")

    # Compatibility adjustment documented in babico/icloud-notes-exporter
    # UPSTREAM.md (derived from this exact upstream commit).
    apply("internal/auth/flow.go",
          'f.idmsaDoAccept(ctx, http.MethodGet, idmsaBase, "application/json", st, nil)',
          'f.idmsaDoAccept(ctx, http.MethodGet, idmsaBase+"/verify/trusteddevice", "application/json", st, nil)')

    print("Upstream patches applied. Note metadata may still be absent if Apple omits it.")


if __name__ == "__main__":
    main()
