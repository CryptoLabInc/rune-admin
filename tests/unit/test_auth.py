"""
Unit tests for authentication and token validation.
Updated for per-user token auth (issue #18).
"""
import pytest
import sys
import os
import time

# Add vault to path
sys.path.insert(0, os.path.join(os.path.dirname(__file__), '../../vault'))

from token_store import (
    TokenStore, RateLimiter, Role,
    TokenNotFoundError, TokenExpiredError, RateLimitError, ScopeError,
)
from vault_core import validate_token, token_store

# Demo token used when no config files or env var set
DEMO_TOKEN = "TOKEN-FOR-DEMONSTRATION-PURPOSES-ONLY-DO-NOT-USE-IN-PRODUCTION"


class TestTokenValidation:
    """Test token validation with per-user token store."""

    def setup_method(self):
        """Reset token store to demo mode for each test."""
        token_store._tokens.clear()
        token_store._tokens_by_user.clear()
        token_store._roles.clear()
        token_store._rate_limiters.clear()
        token_store.load_defaults_with_demo_token()

    def test_valid_demo_token(self):
        """Demo token should return (username, role) tuple."""
        username, role = validate_token(DEMO_TOKEN)
        assert username == "demo"
        assert role.name == "admin"

    def test_invalid_token_raises_error(self):
        """Invalid token should raise TokenNotFoundError."""
        with pytest.raises(TokenNotFoundError):
            validate_token("invalid-token-123")

    def test_empty_token_raises_error(self):
        """Empty token should raise TokenNotFoundError."""
        with pytest.raises(TokenNotFoundError):
            validate_token("")

    def test_token_case_sensitive(self):
        """Token validation should be case-sensitive."""
        with pytest.raises(TokenNotFoundError):
            validate_token(DEMO_TOKEN.lower())

    def test_token_no_whitespace_tolerance(self):
        """Tokens with whitespace should fail."""
        with pytest.raises(TokenNotFoundError):
            validate_token(f" {DEMO_TOKEN} ")

    def test_old_tokens_not_valid(self):
        """Old hardcoded tokens should not work."""
        with pytest.raises(TokenNotFoundError):
            validate_token("envector-team-alpha")

    def test_validate_returns_tuple(self):
        """validate_token should return (username, Role) tuple."""
        result = validate_token(DEMO_TOKEN)
        assert isinstance(result, tuple)
        assert len(result) == 2
        username, role = result
        assert isinstance(username, str)
        assert isinstance(role, Role)


class TestRateLimiter:
    """Test rate limiting functionality."""

    def test_allows_requests_under_limit(self):
        """Requests under limit should be allowed."""
        limiter = RateLimiter(max_requests=5, window_seconds=60)
        for _ in range(5):
            assert limiter.is_allowed("test-client") is True

    def test_blocks_requests_over_limit(self):
        """Requests over limit should be blocked."""
        limiter = RateLimiter(max_requests=3, window_seconds=60)
        for _ in range(3):
            limiter.is_allowed("test-client")
        assert limiter.is_allowed("test-client") is False

    def test_different_clients_have_separate_limits(self):
        """Different clients should have independent rate limits."""
        limiter = RateLimiter(max_requests=2, window_seconds=60)
        limiter.is_allowed("client-a")
        limiter.is_allowed("client-a")
        assert limiter.is_allowed("client-a") is False
        assert limiter.is_allowed("client-b") is True

    def test_window_expiration(self):
        """Old requests should expire after window."""
        limiter = RateLimiter(max_requests=2, window_seconds=1)
        limiter.is_allowed("test-client")
        limiter.is_allowed("test-client")
        assert limiter.is_allowed("test-client") is False
        time.sleep(1.1)
        assert limiter.is_allowed("test-client") is True

    def test_retry_after_returns_correct_value(self):
        """get_retry_after should return seconds until next allowed request."""
        limiter = RateLimiter(max_requests=1, window_seconds=60)
        limiter.is_allowed("test-client")
        retry_after = limiter.get_retry_after("test-client")
        assert 55 <= retry_after <= 60

    def test_remove_client(self):
        """remove() should clear a client's tracking data."""
        limiter = RateLimiter(max_requests=1, window_seconds=60)
        limiter.is_allowed("test-client")
        assert limiter.is_allowed("test-client") is False
        limiter.remove("test-client")
        assert limiter.is_allowed("test-client") is True


class TestScopeEnforcement:
    """Test scope enforcement for roles."""

    def setup_method(self):
        token_store._tokens.clear()
        token_store._tokens_by_user.clear()
        token_store._roles.clear()
        token_store._rate_limiters.clear()
        token_store.load_defaults_with_demo_token()

    def test_admin_scope_allows_all_methods(self):
        """Admin role should allow all standard methods."""
        _, role = validate_token(DEMO_TOKEN)
        # Should not raise
        token_store.check_scope(role, "get_public_key")
        token_store.check_scope(role, "decrypt_scores")
        token_store.check_scope(role, "decrypt_metadata")
        token_store.check_scope(role, "manage_tokens")

    def test_scope_rejects_unauthorized_method(self):
        """Methods not in scope should raise ScopeError."""
        role = Role("limited", ["get_public_key"], 5, "30/60s")
        with pytest.raises(ScopeError, match="decrypt_scores"):
            token_store.check_scope(role, "decrypt_scores")
