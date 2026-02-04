"""
Unit tests for authentication and token validation.
"""
import pytest
import sys
import os

# Add mcp/vault to path
sys.path.insert(0, os.path.join(os.path.dirname(__file__), '../../mcp/vault'))

from vault_mcp import validate_token, VALID_TOKENS


class TestTokenValidation:
    """Test token validation logic."""
    
    def test_valid_token_team_alpha(self):
        """Valid token should not raise exception."""
        try:
            validate_token("envector-team-alpha")
        except ValueError:
            pytest.fail("Valid token raised ValueError")
    
    def test_valid_token_admin(self):
        """Admin token should be accepted."""
        try:
            validate_token("envector-admin-001")
        except ValueError:
            pytest.fail("Valid admin token raised ValueError")
    
    def test_invalid_token_raises_error(self):
        """Invalid token should raise ValueError."""
        with pytest.raises(ValueError, match="Access Denied"):
            validate_token("invalid-token-123")
    
    def test_empty_token_raises_error(self):
        """Empty token should raise ValueError."""
        with pytest.raises(ValueError, match="Access Denied"):
            validate_token("")
    
    def test_none_token_raises_error(self):
        """None token should raise ValueError."""
        with pytest.raises(ValueError, match="Access Denied"):
            validate_token(None)
    
    def test_token_case_sensitive(self):
        """Token validation should be case-sensitive."""
        with pytest.raises(ValueError):
            validate_token("ENVECTOR-TEAM-ALPHA")
    
    def test_token_no_whitespace_tolerance(self):
        """Tokens with whitespace should fail."""
        with pytest.raises(ValueError):
            validate_token(" envector-team-alpha ")
    
    def test_valid_tokens_set_contains_expected(self):
        """VALID_TOKENS should contain expected tokens."""
        assert "envector-team-alpha" in VALID_TOKENS
        assert "envector-admin-001" in VALID_TOKENS
        assert len(VALID_TOKENS) >= 2
