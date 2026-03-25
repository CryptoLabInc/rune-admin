"""
Unit tests for TokenStore: per-user token management, role CRUD, persistence.
"""
import copy
import datetime
import os
import sys
import tempfile

import pytest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), '../../vault'))

from token_store import (
    TokenStore, Role, Token, RateLimiter,
    TokenNotFoundError, TokenExpiredError, RateLimitError,
    TopKExceededError, ScopeError,
)


class TestTokenStore:
    """Test token lifecycle: add, validate, revoke, expiry, rate limit."""

    def setup_method(self):
        self.store = TokenStore()
        self.store._roles = {
            "admin": Role("admin", ["get_public_key", "decrypt_scores", "decrypt_metadata", "manage_tokens"], 50, "150/60s"),
            "member": Role("member", ["get_public_key", "decrypt_scores", "decrypt_metadata"], 10, "30/60s"),
        }

    def test_add_and_validate_token(self):
        tok = self.store.add_token("alice", "member", expires_days=90)
        assert tok.user == "alice"
        assert tok.token.startswith("evt_")
        assert tok.role == "member"

        username, role = self.store.validate(tok.token)
        assert username == "alice"
        assert role.name == "member"

    def test_invalid_token_raises(self):
        with pytest.raises(TokenNotFoundError):
            self.store.validate("nonexistent_token")

    def test_expired_token_raises(self):
        tok = self.store.add_token("bob", "member", expires_days=1)
        # Manually expire the token
        tok.expires = (datetime.date.today() - datetime.timedelta(days=1)).isoformat()
        with pytest.raises(TokenExpiredError, match="bob"):
            self.store.validate(tok.token)

    def test_revoke_token(self):
        tok = self.store.add_token("charlie", "member")
        assert self.store.revoke_token("charlie") is True
        with pytest.raises(TokenNotFoundError):
            self.store.validate(tok.token)

    def test_revoke_nonexistent_returns_false(self):
        assert self.store.revoke_token("nobody") is False

    def test_duplicate_user_rejected(self):
        self.store.add_token("alice", "member")
        with pytest.raises(ValueError, match="already exists"):
            self.store.add_token("alice", "member")

    def test_invalid_role_rejected(self):
        with pytest.raises(ValueError, match="does not exist"):
            self.store.add_token("alice", "nonexistent_role")

    def test_list_tokens_hides_values(self):
        self.store.add_token("alice", "member", expires_days=30)
        result = self.store.list_tokens()
        assert len(result) == 1
        assert result[0]["user"] == "alice"
        # Token value should not be in list output
        assert "token" not in result[0]

    def test_rate_limiting_per_user(self):
        """Rate limiting should use per-role limits keyed by username."""
        # Member role: 30/60s — use a custom role with low limit for test
        self.store.add_role("limited", ["get_public_key"], 5, "2/60s")
        tok = self.store.add_token("ratelimited_user", "limited")

        self.store.validate(tok.token)
        self.store.validate(tok.token)
        with pytest.raises(RateLimitError):
            self.store.validate(tok.token)

    def test_top_k_from_role(self):
        tok = self.store.add_token("alice", "member")
        _, role = self.store.validate(tok.token)
        assert role.top_k == 10  # member default

    def test_never_expires_token(self):
        tok = self.store.add_token("permanent_user", "admin")
        assert tok.expires is None
        assert tok.is_expired is False
        # Should validate fine
        username, _ = self.store.validate(tok.token)
        assert username == "permanent_user"

    def test_legacy_env_loading(self):
        store = TokenStore()
        store.load_legacy_env("token_a,token_b")
        # Should have 2 tokens with admin role
        u1, r1 = store.validate("token_a")
        assert u1 == "legacy_0"
        assert r1.name == "admin"
        u2, _ = store.validate("token_b")
        assert u2 == "legacy_1"

    def test_persist_and_reload(self):
        """Tokens and roles should survive persist → reload cycle."""
        with tempfile.TemporaryDirectory() as tmpdir:
            roles_path = os.path.join(tmpdir, "roles.yml")
            tokens_path = os.path.join(tmpdir, "tokens.yml")

            # Store 1: add data and persist
            store1 = TokenStore()
            store1.load_from_files(roles_path, tokens_path)
            store1.add_role("researcher", ["get_public_key", "decrypt_scores"], 3, "10/60s")
            tok = store1.add_token("alice", "member", expires_days=90)

            # Wait for async persist
            store1._persist_executor.shutdown(wait=True)

            # Store 2: reload from files
            store2 = TokenStore()
            store2.load_from_files(roles_path, tokens_path)

            # Validate alice's token works
            username, role = store2.validate(tok.token)
            assert username == "alice"
            assert role.name == "member"

            # Validate custom role exists
            roles = store2.list_roles()
            role_names = [r["name"] for r in roles]
            assert "researcher" in role_names


class TestRoleCRUD:
    """Test role create, update, delete, list operations."""

    def setup_method(self):
        self.store = TokenStore()
        self.store._roles = {
            "admin": Role("admin", ["get_public_key", "decrypt_scores", "decrypt_metadata", "manage_tokens"], 50, "150/60s"),
            "member": Role("member", ["get_public_key", "decrypt_scores", "decrypt_metadata"], 10, "30/60s"),
        }

    def test_create_role(self):
        role = self.store.add_role(
            "researcher", ["get_public_key", "decrypt_scores"], 3, "10/60s"
        )
        assert role.name == "researcher"
        assert role.top_k == 3
        assert "get_public_key" in role.scope

    def test_create_duplicate_role_rejected(self):
        with pytest.raises(ValueError, match="already exists"):
            self.store.add_role("admin", ["get_public_key"], 5, "30/60s")

    def test_update_role(self):
        role = self.store.update_role("member", top_k=8)
        assert role.top_k == 8
        assert role.name == "member"

    def test_update_nonexistent_role_rejected(self):
        with pytest.raises(ValueError, match="does not exist"):
            self.store.update_role("nonexistent", top_k=5)

    def test_delete_custom_role(self):
        self.store.add_role("temp", ["get_public_key"], 1, "5/60s")
        self.store.delete_role("temp")
        roles = self.store.list_roles()
        assert "temp" not in [r["name"] for r in roles]

    def test_delete_default_role_rejected(self):
        with pytest.raises(ValueError, match="Cannot delete default"):
            self.store.delete_role("admin")
        with pytest.raises(ValueError, match="Cannot delete default"):
            self.store.delete_role("member")

    def test_delete_role_with_active_tokens_rejected(self):
        self.store.add_role("temp", ["get_public_key"], 1, "5/60s")
        self.store.add_token("user1", "temp")
        with pytest.raises(ValueError, match="token for user"):
            self.store.delete_role("temp")

    def test_list_roles(self):
        roles = self.store.list_roles()
        assert len(roles) >= 2
        names = [r["name"] for r in roles]
        assert "admin" in names
        assert "member" in names

    def test_update_role_clears_rate_limiters(self):
        """Changing a role's rate_limit should reset affected rate limiters."""
        tok = self.store.add_token("alice", "member")
        # Validate to create rate limiter
        self.store.validate(tok.token)
        assert "alice" in self.store._rate_limiters

        # Update role
        self.store.update_role("member", rate_limit="100/60s")
        assert "alice" not in self.store._rate_limiters

    def test_role_rate_limit_parsed(self):
        role = Role("test", [], 5, "30/60s")
        max_req, window = role.rate_limit_parsed
        assert max_req == 30
        assert window == 60


class TestScopeCheck:
    """Test scope enforcement."""

    def test_scope_allows_valid_method(self):
        store = TokenStore()
        role = Role("member", ["get_public_key", "decrypt_scores"], 5, "30/60s")
        store.check_scope(role, "get_public_key")  # Should not raise

    def test_scope_rejects_invalid_method(self):
        store = TokenStore()
        role = Role("limited", ["get_public_key"], 5, "30/60s")
        with pytest.raises(ScopeError, match="decrypt_scores"):
            store.check_scope(role, "decrypt_scores")


class TestTopKExceeded:
    """Test TopKExceededError."""

    def test_top_k_exceeded_message(self):
        err = TopKExceededError(15, 10, "admin")
        assert "15" in str(err)
        assert "10" in str(err)
        assert "admin" in str(err)
        assert err.requested == 15
        assert err.max_top_k == 10
