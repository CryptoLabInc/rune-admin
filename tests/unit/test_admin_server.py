"""
Unit tests for Admin HTTP server.
"""
import http.client
import json
import os
import sys
import time

import pytest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), '../../vault'))

from token_store import TokenStore, DEFAULT_ROLES
from admin_server import start_admin_server


def _request(port, method, path, body=None):
    conn = http.client.HTTPConnection("127.0.0.1", port)
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
        self.store = TokenStore()
        self.store._roles = dict(DEFAULT_ROLES)
        # Use port 0 to let OS assign an available ephemeral port
        self.server = start_admin_server(self.store, "127.0.0.1", 0)
        self.port = self.server.server_address[1]
        time.sleep(0.1)  # Give server time to start
        yield
        self.server.shutdown()

    # ── Token endpoints ──────────────────────────────────────────────

    def test_issue_token(self):
        status, data = _request(self.port, "POST", "/tokens", {
            "user": "alice", "role": "member", "expires_days": 90
        })
        assert status == 201
        assert data["user"] == "alice"
        assert data["token"].startswith("evt_")
        assert data["role"] == "member"

    def test_list_tokens(self):
        _request(self.port, "POST", "/tokens", {
            "user": "alice", "role": "member"
        })
        status, data = _request(self.port, "GET", "/tokens")
        assert status == 200
        assert len(data["tokens"]) == 1
        assert data["tokens"][0]["user"] == "alice"

    def test_revoke_token(self):
        _request(self.port, "POST", "/tokens", {
            "user": "alice", "role": "member"
        })
        status, data = _request(self.port, "DELETE", "/tokens/alice")
        assert status == 200
        assert "Revoked" in data["message"]

        # List should be empty
        _, data = _request(self.port, "GET", "/tokens")
        assert len(data["tokens"]) == 0

    def test_revoke_nonexistent_token(self):
        status, data = _request(self.port, "DELETE", "/tokens/nobody")
        assert status == 404

    def test_issue_duplicate_user(self):
        _request(self.port, "POST", "/tokens", {
            "user": "alice", "role": "member"
        })
        status, data = _request(self.port, "POST", "/tokens", {
            "user": "alice", "role": "member"
        })
        assert status == 400
        assert "already exists" in data["error"]

    def test_issue_token_invalid_role(self):
        status, data = _request(self.port, "POST", "/tokens", {
            "user": "alice", "role": "nonexistent"
        })
        assert status == 400

    # ── Rotate endpoints ──────────────────────────────────────────────

    def test_rotate_token(self):
        _, issue_data = _request(self.port, "POST", "/tokens", {
            "user": "alice", "role": "member"
        })
        status, data = _request(self.port, "POST", "/tokens/alice/rotate", {})
        assert status == 200
        assert data["user"] == "alice"
        assert data["token"].startswith("evt_")
        assert data["token"] != issue_data["token"]
        assert data["role"] == "member"

    def test_rotate_nonexistent_user(self):
        status, data = _request(self.port, "POST", "/tokens/nobody/rotate", {})
        assert status == 400
        assert "No token found" in data["error"]

    def test_rotate_all(self):
        _request(self.port, "POST", "/tokens", {"user": "alice", "role": "member"})
        _request(self.port, "POST", "/tokens", {"user": "bob", "role": "admin"})
        status, data = _request(self.port, "POST", "/tokens/_rotate_all", {})
        assert status == 200
        assert data["rotated"] == 2
        assert len(data["tokens"]) == 2

    # ── Role endpoints ───────────────────────────────────────────────

    def test_list_roles(self):
        status, data = _request(self.port, "GET", "/roles")
        assert status == 200
        names = [r["name"] for r in data["roles"]]
        assert "admin" in names
        assert "member" in names

    def test_create_role(self):
        status, data = _request(self.port, "POST", "/roles", {
            "name": "researcher",
            "scope": ["get_public_key", "decrypt_scores"],
            "top_k": 3,
            "rate_limit": "10/60s",
        })
        assert status == 201
        assert data["name"] == "researcher"

    def test_update_role(self):
        status, data = _request(self.port, "PUT", "/roles/member", {
            "top_k": 8,
        })
        assert status == 200
        assert data["top_k"] == 8

    def test_delete_custom_role(self):
        _request(self.port, "POST", "/roles", {
            "name": "temp",
            "scope": ["get_public_key"],
            "top_k": 1,
            "rate_limit": "5/60s",
        })
        status, data = _request(self.port, "DELETE", "/roles/temp")
        assert status == 200

    def test_delete_default_role_rejected(self):
        status, data = _request(self.port, "DELETE", "/roles/admin")
        assert status == 400
        assert "Cannot delete default" in data["error"]

    def test_unknown_resource(self):
        status, _ = _request(self.port, "GET", "/unknown")
        assert status == 404
