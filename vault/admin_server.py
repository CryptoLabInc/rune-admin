"""
Admin HTTP server for token and role management.

Listens on 127.0.0.1:8081 (container-internal only, not exposed via Docker).
No authentication required — access is protected by:
  SSH → docker group → docker exec → container isolation.
"""

import json
import logging
import re
import threading
from http.server import BaseHTTPRequestHandler, HTTPServer

logger = logging.getLogger("vault.admin")

DEFAULT_ADMIN_HOST = "127.0.0.1"
DEFAULT_ADMIN_PORT = 8081


# =============================================================================
# Route table: (method, pattern) → handler name
# Patterns use {name} for path parameters, compiled to regex at import time.
# =============================================================================

_ROUTE_DEFS = [
    ("GET", "/health", "_handle_health"),
    ("GET", "/tokens", "_handle_list_tokens"),
    ("GET", "/roles", "_handle_list_roles"),
    ("POST", "/tokens", "_handle_issue_token"),
    ("POST", "/tokens/{user}/rotate", "_handle_rotate_token"),
    ("POST", "/tokens/_rotate_all", "_handle_rotate_all"),
    ("POST", "/roles", "_handle_create_role"),
    ("PUT", "/roles/{name}", "_handle_update_role"),
    ("DELETE", "/tokens/{user}", "_handle_revoke_token"),
    ("DELETE", "/roles/{name}", "_handle_delete_role"),
]

_ROUTES: list[tuple[str, re.Pattern, list[str], str]] = []
for _method, _pattern, _handler in _ROUTE_DEFS:
    _param_names = re.findall(r"\{(\w+)\}", _pattern)
    _regex = re.compile("^" + re.sub(r"\{(\w+)\}", r"(?P<\1>[^/]+)", _pattern) + "$")
    _ROUTES.append((_method, _regex, _param_names, _handler))


class AdminHandler(BaseHTTPRequestHandler):
    """Request handler for token and role admin API."""

    # Set by start_admin_server() before requests are handled
    token_store = None
    health_servicer = None

    def log_message(self, format, *args):
        logger.info(format, *args)

    def _read_json(self) -> dict:
        length = int(self.headers.get("Content-Length", 0))
        if length == 0:
            return {}
        body = self.rfile.read(length)
        return json.loads(body)

    def _send_json(self, data: dict, status: int = 200):
        body = json.dumps(data).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _send_error(self, status: int, message: str):
        self._send_json({"error": message}, status)

    # ── Routing ──────────────────────────────────────────────────────────

    def _dispatch(self, method: str):
        path = self.path.rstrip("/") or "/"
        for route_method, regex, _, handler_name in _ROUTES:
            if route_method != method:
                continue
            m = regex.match(path)
            if m:
                handler = getattr(self, handler_name)
                try:
                    kwargs = m.groupdict()
                    if method in ("POST", "PUT"):
                        kwargs["body"] = self._read_json()
                    handler(**kwargs)
                except (ValueError, KeyError) as e:
                    self._send_error(400, str(e))
                except Exception as e:
                    self._send_error(500, str(e))
                return
        self._send_error(404, f"No route for {method} {self.path}")

    def do_GET(self):
        self._dispatch("GET")

    def do_POST(self):
        self._dispatch("POST")

    def do_PUT(self):
        self._dispatch("PUT")

    def do_DELETE(self):
        self._dispatch("DELETE")

    # ── Health ────────────────────────────────────────────────────────────

    def _handle_health(self):
        from grpc_health.v1 import health_pb2

        if self.health_servicer is not None:
            resp = self.health_servicer.Check(health_pb2.HealthCheckRequest(service=""), None)
            if resp.status != health_pb2.HealthCheckResponse.SERVING:
                self._send_json({"status": "unhealthy"}, 503)
                return
        self._send_json({"status": "ok"})

    # ── Token handlers ───────────────────────────────────────────────────

    def _handle_list_tokens(self):
        self._send_json({"tokens": self.token_store.list_tokens()})

    def _handle_issue_token(self, body: dict):
        user = body.get("user")
        role = body.get("role")
        if not user or not role:
            self._send_error(400, "Missing required fields: user, role")
            return
        expires_days = body.get("expires_days")
        tok = self.token_store.add_token(user, role, expires_days)
        self._send_json(
            {
                "user": tok.user,
                "token": tok.token,
                "role": tok.role,
                "issued_at": tok.issued_at,
                "expires": tok.expires or "never",
            },
            201,
        )

    def _handle_revoke_token(self, user: str):
        revoked = self.token_store.revoke_token(user)
        if revoked:
            self._send_json({"message": f"Revoked token for '{user}'"})
        else:
            self._send_error(404, f"No token found for user '{user}'")

    def _handle_rotate_token(self, user: str, body: dict):
        tok = self.token_store.rotate_token(user)
        self._send_json(
            {
                "user": tok.user,
                "token": tok.token,
                "role": tok.role,
                "issued_at": tok.issued_at,
                "expires": tok.expires or "never",
            }
        )

    def _handle_rotate_all(self, body: dict):
        tokens = self.token_store.rotate_all_tokens()
        self._send_json(
            {
                "rotated": len(tokens),
                "tokens": [{"user": t.user, "token": t.token, "role": t.role} for t in tokens],
            }
        )

    # ── Role handlers ────────────────────────────────────────────────────

    def _handle_list_roles(self):
        self._send_json({"roles": self.token_store.list_roles()})

    def _handle_create_role(self, body: dict):
        name = body.get("name")
        scope = body.get("scope")
        top_k = body.get("top_k")
        rate_limit = body.get("rate_limit")
        if not all([name, scope, top_k is not None, rate_limit]):
            self._send_error(400, "Missing required fields: name, scope, top_k, rate_limit")
            return
        role = self.token_store.add_role(name, scope, top_k, rate_limit)
        self._send_json(
            {
                "name": role.name,
                "scope": role.scope,
                "top_k": role.top_k,
                "rate_limit": role.rate_limit,
            },
            201,
        )

    def _handle_update_role(self, name: str, body: dict):
        kwargs = {}
        if "scope" in body:
            kwargs["scope"] = body["scope"]
        if "top_k" in body:
            kwargs["top_k"] = body["top_k"]
        if "rate_limit" in body:
            kwargs["rate_limit"] = body["rate_limit"]
        if not kwargs:
            self._send_error(400, "No fields to update")
            return
        role = self.token_store.update_role(name, **kwargs)
        self._send_json(
            {
                "name": role.name,
                "scope": role.scope,
                "top_k": role.top_k,
                "rate_limit": role.rate_limit,
            }
        )

    def _handle_delete_role(self, name: str):
        self.token_store.delete_role(name)
        self._send_json({"message": f"Deleted role '{name}'"})


def start_admin_server(
    store, host: str = DEFAULT_ADMIN_HOST, port: int = DEFAULT_ADMIN_PORT, health_servicer=None
):
    """Start the admin HTTP server in a daemon thread."""
    AdminHandler.token_store = store
    AdminHandler.health_servicer = health_servicer
    server = HTTPServer((host, port), AdminHandler)
    thread = threading.Thread(target=server.serve_forever, daemon=True, name="admin-server")
    thread.start()
    logger.info("Admin server started on %s:%d", host, port)
    return server
