#!/usr/bin/env python3
"""Provision one Mint resource and one local user against Authplane admin API.
Run as root on the VPS, NOT on the ChatGPT host. Does not touch nginx or notes-mcp.
"""
import getpass
import json
import os
import re
import sys
from pathlib import Path
from urllib.error import HTTPError, URLError
from urllib.parse import urlsplit
from urllib.request import Request, urlopen

ADMIN = "http://127.0.0.1:9001"
SECRETS = Path("/etc/authplane/secrets.env")
ENV_PATH = Path("/etc/notes-gateway/gateway.env")


def abort(message):
    sys.exit("ERROR: " + message)


def load_issuer():
    value = os.environ.get("AUTHPLANE_ISSUER", "").strip().rstrip("/")
    parsed = urlsplit(value)
    if (parsed.scheme != "https" or not parsed.hostname or parsed.username
            or parsed.password or parsed.path or parsed.query or parsed.fragment):
        abort("set AUTHPLANE_ISSUER to an HTTPS origin such as https://mcp.example.com")
    return value


ISSUER = load_issuer()
RESOURCE = ISSUER + "/mcp"


def load_admin_key():
    if os.geteuid() != 0:
        abort("run with sudo; root is required to read the local admin key")
    raw = SECRETS.read_text(encoding="utf8")
    vals = {}
    for line in raw.splitlines():
        if "=" in line and not line.lstrip().startswith("#"):
            k, v = line.split("=", 1)
            vals[k.strip()] = v.strip().strip('"')
    key = vals.get("AUTHPLANE_ADMIN_API_KEY", "")
    if len(key) < 24:
        abort("admin key absent/too short; will not perform admin operations")
    return key


ADMIN_KEY = ""


def api(method, path, data=None):
    payload = None if data is None else json.dumps(data, ensure_ascii=True).encode("utf8")
    req = Request(ADMIN + path, data=payload, method=method, headers={
        "Authorization": "Bearer " + ADMIN_KEY,
        "Accept": "application/json",
        "Content-Type": "application/json",
    })
    try:
        with urlopen(req, timeout=15) as response:
            return json.load(response)
    except HTTPError as exc:
        abort("Authplane local admin API %s %s returned HTTP %s" % (method, path, exc.code))
    except (URLError, TimeoutError) as exc:
        abort("Authplane local admin API is unreachable: %s" % type(exc).__name__)


def ensure_resource():
    resources = api("GET", "/admin/resources")
    if not isinstance(resources, list):
        abort("unexpected /admin/resources JSON; stop instead of creating duplicates")
    matches = [r for r in resources if r.get("uri") == RESOURCE]
    if len(matches) > 1:
        abort("multiple resources with the same URI")
    if matches:
        r = matches[0]
        if r.get("backend_kind") != "mint":
            abort("existing resource URI conflicts with required backend kind")
        scopes = r.get("scopes", [])
        if "notes:read" not in [s.get("name") for s in scopes if isinstance(s, dict)]:
            abort("existing resource does not have notes:read; manual review required")
        print("Resource already exists with expected URI and read scope")
        return
    created = api("POST", "/admin/resources", {
        "slug": "apple-notes",
        "uri": RESOURCE,
        "backend_kind": "mint",
        "display_name": "Private Apple Notes (read only)",
        "scopes": [{"name": "notes:read", "upstream": "", "description": "Read selected Apple Notes"}],
    })
    if not isinstance(created, dict) or created.get("uri") != RESOURCE:
        abort("resource response does not confirm expected URI")
    print("Resource created: apple-notes -> " + RESOURCE)


def ensure_user():
    users = api("GET", "/admin/users")
    if not isinstance(users, list):
        abort("unexpected /admin/users JSON; stop instead of creating duplicates")
    email = input("OAuth sign-in email (only stored locally on VPS): ").strip().lower()
    if not email or len(email) > 254 or "@" not in email or "\n" in email:
        abort("invalid email")
    matches = [u for u in users if str(u.get("email", "")).lower() == email]
    if len(matches) > 1:
        abort("multiple users with this email")
    if matches:
        user = matches[0]
        print("Existing Authplane user reused (password was not changed)")
    else:
        name = input("Display name [Notes Owner]: ").strip() or "Notes Owner"
        pwd = getpass.getpass("New OAuth password (at least 16 chars): ")
        again = getpass.getpass("Confirm password: ")
        if len(pwd) < 16 or pwd != again:
            abort("password too short or confirmation differs")
        user = api("POST", "/admin/users", {
            "email": email,
            "name": name,
            "password": pwd,
            "role": "user",
        })
        pwd = ""
        again = ""
        print("OAuth user created")
    uid = user.get("id", "")
    if not isinstance(uid, str) or not re.fullmatch(r"[A-Za-z0-9_-]{8,128}", uid):
        abort("unexpected user id; will not set access allowlist")
    if user.get("role") not in ("user", "admin"):
        abort("unexpected OAuth user role")
    return uid


def write_gateway_config(user_id):
    ENV_PATH.parent.mkdir(parents=True, mode=0o700, exist_ok=True)
    os.chmod(ENV_PATH.parent, 0o700)
    value = "\n".join([
        "AUTHPLANE_ISSUER=" + ISSUER,
        "AUTHPLANE_RESOURCE=" + RESOURCE,
        "AUTH_ALLOWED_SUBJECT=" + user_id,
        "GATEWAY_LISTEN=127.0.0.1:8766",
        "MCP_UPSTREAM=http://127.0.0.1:8765",
        "",
    ])
    if ENV_PATH.exists():
        if ENV_PATH.read_text(encoding="utf8") != value:
            abort("gateway.env already exists with different values; manual review required")
        print("Existing gateway allowlist matches; no rewrite")
        return
    with ENV_PATH.open("x", encoding="utf8") as f:
        f.write(value)
    os.chmod(ENV_PATH, 0o600)
    print("Access allowlist saved to /etc/notes-gateway/gateway.env (root-only)")


def main():
    global ADMIN_KEY
    ADMIN_KEY = load_admin_key()
    ensure_resource()
    user_id = ensure_user()
    write_gateway_config(user_id)
    # Do not print email, password, admin key, or user ID.
    print("Provisioning succeeded. Public /mcp should still return HTTP 403.")


if __name__ == "__main__":
    main()
