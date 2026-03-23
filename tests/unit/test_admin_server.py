"""
Unit tests for Admin HTTP server (unix socket API).
"""
import http.client
import json
import os
import socket
import sys
import tempfile
import time

import pytest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), '../../vault'))

from token_store import TokenStore, DEFAULT_ROLES
from admin_server import start_admin_server


class UnixHTTPConnection(http.client.HTTPConnection):
    """HTTP connection over a Unix domain socket for testing."""
    def __init__(self, socket_path: str):
        super().__init__("localhost")
        self.socket_path = socket_path

    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.connect(self.socket_path)


def _request(sock_path, method, path, body=None):
    conn = UnixHTTPConnection(sock_path)
    headers = {"Content-Type": "application/json"} if body else {}
    data = json.dumps(body).encode() if body else None
    conn.request(method, path, body=data, headers=headers)
    resp = conn.getresponse()
    result = json.loads(resp.read().decode())
    conn.close()
    return resp.status, result


class TestAdminServer:
    """Integration tests for the admin HTTP API."""

    @pytest.fixture(autouse=True)
    def setup_server(self):
        # Use /tmp for short path (macOS AF_UNIX 104-char limit)
        self.sock_path = f"/tmp/vault-test-{os.getpid()}.sock"
        self.store = TokenStore()
        self.store._roles = dict(DEFAULT_ROLES)
        self.server = start_admin_server(self.store, self.sock_path)
        time.sleep(0.1)  # Give server time to start
        yield
        self.server.shutdown()
        if os.path.exists(self.sock_path):
            os.unlink(self.sock_path)

    # ── Token endpoints ──────────────────────────────────────────────

    def test_issue_token(self):
        status, data = _request(self.sock_path, "POST", "/tokens", {
            "user": "alice", "role": "agent", "expires_days": 90
        })
        assert status == 201
        assert data["user"] == "alice"
        assert data["token"].startswith("evt_")
        assert data["role"] == "agent"

    def test_list_tokens(self):
        _request(self.sock_path, "POST", "/tokens", {
            "user": "alice", "role": "agent"
        })
        status, data = _request(self.sock_path, "GET", "/tokens")
        assert status == 200
        assert len(data["tokens"]) == 1
        assert data["tokens"][0]["user"] == "alice"

    def test_revoke_token(self):
        _request(self.sock_path, "POST", "/tokens", {
            "user": "alice", "role": "agent"
        })
        status, data = _request(self.sock_path, "DELETE", "/tokens/alice")
        assert status == 200
        assert "Revoked" in data["message"]

        # List should be empty
        _, data = _request(self.sock_path, "GET", "/tokens")
        assert len(data["tokens"]) == 0

    def test_revoke_nonexistent_token(self):
        status, data = _request(self.sock_path, "DELETE", "/tokens/nobody")
        assert status == 404

    def test_issue_duplicate_user(self):
        _request(self.sock_path, "POST", "/tokens", {
            "user": "alice", "role": "agent"
        })
        status, data = _request(self.sock_path, "POST", "/tokens", {
            "user": "alice", "role": "agent"
        })
        assert status == 400
        assert "already exists" in data["error"]

    def test_issue_token_invalid_role(self):
        status, data = _request(self.sock_path, "POST", "/tokens", {
            "user": "alice", "role": "nonexistent"
        })
        assert status == 400

    # ── Role endpoints ───────────────────────────────────────────────

    def test_list_roles(self):
        status, data = _request(self.sock_path, "GET", "/roles")
        assert status == 200
        names = [r["name"] for r in data["roles"]]
        assert "admin" in names
        assert "agent" in names

    def test_create_role(self):
        status, data = _request(self.sock_path, "POST", "/roles", {
            "name": "researcher",
            "scope": ["get_public_key", "decrypt_scores"],
            "top_k": 3,
            "rate_limit": "10/60s",
        })
        assert status == 201
        assert data["name"] == "researcher"

    def test_update_role(self):
        status, data = _request(self.sock_path, "PUT", "/roles/agent", {
            "top_k": 8,
        })
        assert status == 200
        assert data["top_k"] == 8

    def test_delete_custom_role(self):
        _request(self.sock_path, "POST", "/roles", {
            "name": "temp",
            "scope": ["get_public_key"],
            "top_k": 1,
            "rate_limit": "5/60s",
        })
        status, data = _request(self.sock_path, "DELETE", "/roles/temp")
        assert status == 200

    def test_delete_default_role_rejected(self):
        status, data = _request(self.sock_path, "DELETE", "/roles/admin")
        assert status == 400
        assert "Cannot delete default" in data["error"]

    def test_unknown_resource(self):
        status, _ = _request(self.sock_path, "GET", "/unknown")
        assert status == 404
