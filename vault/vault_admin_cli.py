#!/usr/bin/env python3
"""
Vault Admin CLI — manages per-user tokens and roles.

Usage (via docker exec or runevault alias):
    runevault token issue --user alice --role agent --expires 90d
    runevault token revoke --user alice
    runevault token list
    runevault role list
    runevault role create --name researcher --scope get_public_key,decrypt_scores --top-k 3 --rate-limit 10/60s
    runevault role update --name agent --top-k 8
    runevault role delete --name researcher
"""

import argparse
import http.client
import json
import re
import sys

ADMIN_HOST = "127.0.0.1"
ADMIN_PORT = 8081


def _request(method: str, path: str, body: dict | None = None) -> dict:
    """Send an HTTP request to the admin server and return parsed JSON."""
    conn = http.client.HTTPConnection(ADMIN_HOST, ADMIN_PORT)
    headers = {"Content-Type": "application/json"} if body else {}
    data = json.dumps(body).encode() if body else None
    try:
        conn.request(method, path, body=data, headers=headers)
        resp = conn.getresponse()
        result = json.loads(resp.read().decode())
        if resp.status >= 400:
            print(f"Error: {result.get('error', 'Unknown error')}", file=sys.stderr)
            sys.exit(1)
        return result
    except ConnectionRefusedError:
        print("Error: Cannot connect to admin server. Is Vault running?", file=sys.stderr)
        sys.exit(1)
    finally:
        conn.close()


# ── Token commands ───────────────────────────────────────────────────────

def _parse_duration(value: str) -> int:
    """Parse duration string like '90d', '12w', '6m' into days."""
    m = re.fullmatch(r"(\d+)([dwm])", value)
    if not m:
        print(f"Error: Invalid duration '{value}'. Use <number><d|w|m> (e.g. 90d, 12w, 6m)", file=sys.stderr)
        sys.exit(1)
    n, unit = int(m.group(1)), m.group(2)
    if unit == "d":
        return n
    if unit == "w":
        return n * 7
    return n * 30  # 'm' approximation


def cmd_token_issue(args):
    body = {"user": args.user, "role": args.role}
    if args.expires is not None:
        body["expires_days"] = _parse_duration(args.expires)
    result = _request("POST", "/tokens", body)
    print(f"\nToken issued for '{result['user']}':")
    print(f"  Role:    {result['role']}")
    print(f"  Expires: {result['expires']}")
    print(f"\n  Token: {result['token']}")
    print(f"\n  WARNING: This token will NOT be shown again. Share it securely.")


def cmd_token_revoke(args):
    result = _request("DELETE", f"/tokens/{args.user}")
    print(result["message"])


def cmd_token_rotate(args):
    if args.rotate_all:
        result = _request("POST", "/tokens/_rotate_all", {})
        count = result["rotated"]
        if count == 0:
            print("No tokens to rotate.")
            return
        print(f"Rotated {count} token(s):\n")
        for t in result["tokens"]:
            print(f"  {t['user']}: {t['token']}")
        print(f"\n  WARNING: These tokens will NOT be shown again. Share them securely.")
    else:
        result = _request("POST", f"/tokens/{args.user}/rotate", {})
        print(f"\nToken rotated for '{result['user']}':")
        print(f"  Role:    {result['role']}")
        print(f"  Expires: {result['expires']}")
        print(f"\n  Token: {result['token']}")
        print(f"\n  WARNING: This token will NOT be shown again. Share it securely.")


def cmd_token_list(args):
    result = _request("GET", "/tokens")
    tokens = result.get("tokens", [])
    if not tokens:
        print("No tokens issued.")
        return
    # Table header
    fmt = "{:<16} {:<10} {:>6} {:>10}  {:<12}"
    print(fmt.format("USER", "ROLE", "TOP_K", "RATE", "EXPIRES"))
    for t in tokens:
        print(fmt.format(
            t["user"], t["role"], str(t["top_k"]),
            str(t["rate_limit"]), t["expires"],
        ))


# ── Role commands ────────────────────────────────────────────────────────

def cmd_role_list(args):
    result = _request("GET", "/roles")
    roles = result.get("roles", [])
    if not roles:
        print("No roles defined.")
        return
    fmt = "{:<12} {:<50} {:>6} {:>10}"
    print(fmt.format("ROLE", "SCOPE", "TOP_K", "RATE"))
    for r in roles:
        scope_str = ",".join(r["scope"])
        print(fmt.format(r["name"], scope_str, str(r["top_k"]), r["rate_limit"]))


def cmd_role_create(args):
    scope = [s.strip() for s in args.scope.split(",")]
    body = {
        "name": args.name,
        "scope": scope,
        "top_k": args.top_k,
        "rate_limit": args.rate_limit,
    }
    _request("POST", "/roles", body)
    print(f"Role '{args.name}' created.")


def cmd_role_update(args):
    body = {}
    if args.scope is not None:
        body["scope"] = [s.strip() for s in args.scope.split(",")]
    if args.top_k is not None:
        body["top_k"] = args.top_k
    if args.rate_limit is not None:
        body["rate_limit"] = args.rate_limit
    if not body:
        print("Error: No fields to update.", file=sys.stderr)
        sys.exit(1)
    _request("PUT", f"/roles/{args.name}", body)
    print(f"Role '{args.name}' updated. Changes take effect immediately for all tokens with this role.")


def cmd_role_delete(args):
    _request("DELETE", f"/roles/{args.name}")
    print(f"Role '{args.name}' deleted.")


# ── Argument parsing ─────────────────────────────────────────────────────

def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="runevault",
        description="Rune-Vault Admin CLI",
    )
    sub = parser.add_subparsers(dest="resource", required=True)

    # ── token ──
    token_parser = sub.add_parser("token", help="Manage per-user tokens")
    token_sub = token_parser.add_subparsers(dest="action", required=True)

    issue_p = token_sub.add_parser("issue", help="Issue a new token")
    issue_p.add_argument("--user", required=True, help="Username")
    issue_p.add_argument("--role", required=True, help="Role name")
    issue_p.add_argument("--expires", default=None, help="Duration until expiry (e.g. 90d, 12w, 6m)")
    issue_p.set_defaults(func=cmd_token_issue)

    revoke_p = token_sub.add_parser("revoke", help="Revoke a user's token")
    revoke_p.add_argument("--user", required=True, help="Username")
    revoke_p.set_defaults(func=cmd_token_revoke)

    rotate_p = token_sub.add_parser("rotate", help="Rotate a token (revoke + reissue)")
    rotate_group = rotate_p.add_mutually_exclusive_group(required=True)
    rotate_group.add_argument("--user", help="Username to rotate")
    rotate_group.add_argument("--all", action="store_true", dest="rotate_all", help="Rotate all tokens")
    rotate_p.set_defaults(func=cmd_token_rotate)

    list_p = token_sub.add_parser("list", help="List all tokens")
    list_p.set_defaults(func=cmd_token_list)

    # ── role ──
    role_parser = sub.add_parser("role", help="Manage roles")
    role_sub = role_parser.add_subparsers(dest="action", required=True)

    rlist_p = role_sub.add_parser("list", help="List all roles")
    rlist_p.set_defaults(func=cmd_role_list)

    create_p = role_sub.add_parser("create", help="Create a new role")
    create_p.add_argument("--name", required=True, help="Role name")
    create_p.add_argument("--scope", required=True, help="Comma-separated scope list")
    create_p.add_argument("--top-k", type=int, required=True, help="Max top_k")
    create_p.add_argument("--rate-limit", required=True, help="Rate limit (e.g. 30/60s)")
    create_p.set_defaults(func=cmd_role_create)

    update_p = role_sub.add_parser("update", help="Update a role")
    update_p.add_argument("--name", required=True, help="Role name")
    update_p.add_argument("--scope", default=None, help="Comma-separated scope list")
    update_p.add_argument("--top-k", type=int, default=None, help="Max top_k")
    update_p.add_argument("--rate-limit", default=None, help="Rate limit (e.g. 30/60s)")
    update_p.set_defaults(func=cmd_role_update)

    delete_p = role_sub.add_parser("delete", help="Delete a role")
    delete_p.add_argument("--name", required=True, help="Role name")
    delete_p.set_defaults(func=cmd_role_delete)

    return parser


def main():
    parser = build_parser()
    args = parser.parse_args()
    if hasattr(args, "func"):
        args.func(args)
    else:
        parser.print_help()


if __name__ == "__main__":
    main()
