"""
Unit tests for authentication and token validation.
"""
import pytest
import sys
import os
import time

# Add mcp/vault to path
sys.path.insert(0, os.path.join(os.path.dirname(__file__), '../../mcp/vault'))

from vault_mcp import validate_token, VALID_TOKENS, rate_limiter, RateLimiter

# Demo tokens used when VAULT_TOKENS env var is not set
DEMO_TOKEN = "TOKEN-FOR-DEMONSTRATION-PURPOSES-ONLY-DO-NOT-USE-IN-PRODUCTION"
DEMO_ADMIN_TOKEN = "TOKEN-FOR-DEMONSTRATION-PURPOSES-ONLY-DO-NOT-USE-IN-PRODUCTION"


class TestTokenValidation:
    """Test token validation logic."""

    def test_valid_demo_token(self):
        """Demo token should not raise exception."""
        # Reset rate limiter for clean test
        rate_limiter._requests.clear()
        try:
            validate_token(DEMO_TOKEN)
        except ValueError:
            pytest.fail("Valid demo token raised ValueError")

    def test_valid_demo_admin_token(self):
        """Demo admin token should be accepted."""
        rate_limiter._requests.clear()
        try:
            validate_token(DEMO_ADMIN_TOKEN)
        except ValueError:
            pytest.fail("Valid demo admin token raised ValueError")

    def test_invalid_token_raises_error(self):
        """Invalid token should raise ValueError."""
        rate_limiter._requests.clear()
        with pytest.raises(ValueError, match="Access Denied"):
            validate_token("invalid-token-123")

    def test_empty_token_raises_error(self):
        """Empty token should raise ValueError."""
        rate_limiter._requests.clear()
        with pytest.raises(ValueError, match="Access Denied"):
            validate_token("")

    def test_none_token_raises_error(self):
        """None token should raise ValueError."""
        rate_limiter._requests.clear()
        with pytest.raises(ValueError, match="Access Denied"):
            validate_token(None)

    def test_token_case_sensitive(self):
        """Token validation should be case-sensitive."""
        rate_limiter._requests.clear()
        with pytest.raises(ValueError):
            validate_token("demo-token-get-your-own-at-envector-io")

    def test_token_no_whitespace_tolerance(self):
        """Tokens with whitespace should fail."""
        rate_limiter._requests.clear()
        with pytest.raises(ValueError):
            validate_token(f" {DEMO_TOKEN} ")

    def test_valid_tokens_set_contains_demo_tokens(self):
        """VALID_TOKENS should contain demo tokens when env var not set."""
        # This test assumes VAULT_TOKENS env var is not set
        if not os.getenv("VAULT_TOKENS"):
            assert DEMO_TOKEN in VALID_TOKENS
        assert len(VALID_TOKENS) >= 1

    def test_old_tokens_not_valid(self):
        """Old hardcoded tokens should no longer work."""
        rate_limiter._requests.clear()
        with pytest.raises(ValueError, match="Access Denied"):
            validate_token("envector-team-alpha")
        with pytest.raises(ValueError, match="Access Denied"):
            validate_token("envector-admin-001")

    @pytest.mark.skipif(
        os.getenv("VAULT_TOKENS") is not None,
        reason="Test only applies when VAULT_TOKENS env var is not set (demo mode)"
    )
    def test_demo_tokens_work_when_env_var_not_set(self):
        """Demo tokens should pass validation when VAULT_TOKENS env var is not set."""
        # This test verifies the inverse case of test_old_tokens_not_valid:
        # While old tokens are rejected, new demo tokens should be accepted
        # when running in demo mode (VAULT_TOKENS not set)
        rate_limiter._requests.clear()
        validate_token(DEMO_TOKEN)
        validate_token(DEMO_ADMIN_TOKEN)


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
        # client-a is at limit
        assert limiter.is_allowed("client-a") is False
        # client-b should still be allowed
        assert limiter.is_allowed("client-b") is True

    def test_window_expiration(self):
        """Old requests should expire after window."""
        limiter = RateLimiter(max_requests=2, window_seconds=1)
        limiter.is_allowed("test-client")
        limiter.is_allowed("test-client")
        assert limiter.is_allowed("test-client") is False
        # Wait for window to expire
        time.sleep(1.1)
        assert limiter.is_allowed("test-client") is True

    def test_retry_after_returns_correct_value(self):
        """get_retry_after should return seconds until next allowed request."""
        limiter = RateLimiter(max_requests=1, window_seconds=60)
        limiter.is_allowed("test-client")
        retry_after = limiter.get_retry_after("test-client")
        assert 55 <= retry_after <= 60

    def test_validate_token_rate_limited(self):
        """validate_token should enforce rate limiting."""
        # Create a fresh rate limiter with low limit for testing
        test_limiter = RateLimiter(max_requests=2, window_seconds=60)
        import vault_mcp
        original_limiter = vault_mcp.rate_limiter
        vault_mcp.rate_limiter = test_limiter

        try:
            validate_token(DEMO_TOKEN)
            validate_token(DEMO_TOKEN)
            with pytest.raises(ValueError, match="Rate limit exceeded"):
                validate_token(DEMO_TOKEN)
        finally:
            vault_mcp.rate_limiter = original_limiter
