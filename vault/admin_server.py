"""
Admin HTTP server for token and role management.

Listens on 127.0.0.1:8081 (container-internal only, not exposed via Docker).
No authentication required — access is protected by:
  SSH → docker group → docker exec → container isolation.
"""

import json
import logging
import threading
from http.server import HTTPServer, BaseHTTPRequestHandler

logger = logging.getLogger("vault.admin")

DEFAULT_ADMIN_HOST = "127.0.0.1"
DEFAULT_ADMIN_PORT = 8081


class AdminHandler(BaseHTTPRequestHandler):
    """Request handler for token and role admin API."""

    # Set by start_admin_server() before requests are handled
    token_store = None

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

    def _parse_path(self) -> tuple[str, str | None]:
        """Parse path into (resource, identifier). e.g. /tokens/alice -> ('tokens', 'alice')"""
        parts = self.path.strip("/").split("/", 1)
        resource = parts[0] if parts else ""
        identifier = parts[1] if len(parts) > 1 else None
        return resource, identifier

    def do_GET(self):
        resource, _ = self._parse_path()
        try:
            if resource == "tokens":
                self._send_json({"tokens": self.token_store.list_tokens()})
            elif resource == "roles":
                self._send_json({"roles": self.token_store.list_roles()})
            else:
                self._send_error(404, f"Unknown resource: {resource}")
        except Exception as e:
            self._send_error(500, str(e))

    def do_POST(self):
        resource, _ = self._parse_path()
        try:
            body = self._read_json()
            if resource == "tokens":
                self._handle_issue_token(body)
            elif resource == "roles":
                self._handle_create_role(body)
            else:
                self._send_error(404, f"Unknown resource: {resource}")
        except (ValueError, KeyError) as e:
            self._send_error(400, str(e))
        except Exception as e:
            self._send_error(500, str(e))

    def do_PUT(self):
        resource, identifier = self._parse_path()
        try:
            body = self._read_json()
            if resource == "roles" and identifier:
                self._handle_update_role(identifier, body)
            else:
                self._send_error(404, f"Unknown resource: {resource}/{identifier}")
        except (ValueError, KeyError) as e:
            self._send_error(400, str(e))
        except Exception as e:
            self._send_error(500, str(e))

    def do_DELETE(self):
        resource, identifier = self._parse_path()
        try:
            if resource == "tokens" and identifier:
                self._handle_revoke_token(identifier)
            elif resource == "roles" and identifier:
                self._handle_delete_role(identifier)
            else:
                self._send_error(404, f"Unknown resource: {resource}/{identifier}")
        except ValueError as e:
            self._send_error(400, str(e))
        except Exception as e:
            self._send_error(500, str(e))

    # ── Token handlers ───────────────────────────────────────────────────

    def _handle_issue_token(self, body: dict):
        user = body.get("user")
        role = body.get("role")
        if not user or not role:
            self._send_error(400, "Missing required fields: user, role")
            return
        expires_days = body.get("expires_days")
        tok = self.token_store.add_token(user, role, expires_days)
        self._send_json({
            "user": tok.user,
            "token": tok.token,
            "role": tok.role,
            "created": tok.created,
            "expires": tok.expires or "never",
        }, 201)

    def _handle_revoke_token(self, user: str):
        revoked = self.token_store.revoke_token(user)
        if revoked:
            self._send_json({"message": f"Revoked token for '{user}'"})
        else:
            self._send_error(404, f"No token found for user '{user}'")

    # ── Role handlers ────────────────────────────────────────────────────

    def _handle_create_role(self, body: dict):
        name = body.get("name")
        scope = body.get("scope")
        top_k = body.get("top_k")
        rate_limit = body.get("rate_limit")
        if not all([name, scope, top_k is not None, rate_limit]):
            self._send_error(400, "Missing required fields: name, scope, top_k, rate_limit")
            return
        role = self.token_store.add_role(name, scope, top_k, rate_limit)
        self._send_json({
            "name": role.name,
            "scope": role.scope,
            "top_k": role.top_k,
            "rate_limit": role.rate_limit,
        }, 201)

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
        self._send_json({
            "name": role.name,
            "scope": role.scope,
            "top_k": role.top_k,
            "rate_limit": role.rate_limit,
        })

    def _handle_delete_role(self, name: str):
        self.token_store.delete_role(name)
        self._send_json({"message": f"Deleted role '{name}'"})


def start_admin_server(store, host: str = DEFAULT_ADMIN_HOST, port: int = DEFAULT_ADMIN_PORT):
    """Start the admin HTTP server in a daemon thread."""
    AdminHandler.token_store = store
    server = HTTPServer((host, port), AdminHandler)
    thread = threading.Thread(target=server.serve_forever, daemon=True, name="admin-server")
    thread.start()
    logger.info("Admin server started on %s:%d", host, port)
    return server
