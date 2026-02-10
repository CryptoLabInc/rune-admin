"""
Unit tests for get_public_key MCP tool.
"""
import pytest
import sys
import os
import json
import tempfile
import shutil
from pathlib import Path

# Add mcp/vault to path
sys.path.insert(0, os.path.join(os.path.dirname(__file__), '../../mcp/vault'))

# Import the implementation function (not the MCP-decorated version)
from vault_mcp import _get_public_key_impl as get_public_key, rate_limiter
import vault_mcp
from pyenvector.crypto import KeyGenerator


class TestGetPublicKey:

    @pytest.fixture(autouse=True)
    def reset_rate_limiter(self):
        """Reset rate limiter before each test."""
        rate_limiter._requests.clear()
    """Test get_public_key MCP tool."""
    
    @pytest.fixture(scope="class")
    def test_keys(self):
        """Generate test keys."""
        temp_dir = tempfile.mkdtemp(prefix="test_pubkey_")
        keygen = KeyGenerator(key_path=temp_dir, key_id="test-pubkey", dim_list=[1024])
        keygen.generate_keys()
        
        yield temp_dir
        shutil.rmtree(temp_dir, ignore_errors=True)
    
    def test_valid_token_returns_bundle(self, test_keys, monkeypatch):
        """Valid token should return public key bundle."""
        monkeypatch.setattr('vault_mcp.KEY_DIR', test_keys)
        
        result = get_public_key("DEMO-TOKEN-GET-YOUR-OWN-AT-ENVECTOR-IO")
        
        # Should be valid JSON
        bundle = json.loads(result)
        
        # Should contain public keys
        assert "EncKey.json" in bundle
        assert "EvalKey.json" in bundle
        assert "MetadataKey.json" in bundle
        
        # Should NOT contain secret key
        assert "SecKey.json" not in bundle
    
    def test_invalid_token_raises_error(self, test_keys, monkeypatch):
        """Invalid token should raise ValueError."""
        monkeypatch.setattr('vault_mcp.KEY_DIR', test_keys)
        
        with pytest.raises(ValueError, match="Access Denied"):
            get_public_key("invalid-token")
    
    def test_returned_keys_are_valid_json(self, test_keys, monkeypatch):
        """Each key in bundle should be valid JSON."""
        monkeypatch.setattr('vault_mcp.KEY_DIR', test_keys)
        
        result = get_public_key("DEMO-TOKEN-GET-YOUR-OWN-AT-ENVECTOR-IO")
        bundle = json.loads(result)
        
        for key_name, key_content in bundle.items():
            # Each key should be parseable JSON
            try:
                json.loads(key_content)
            except json.JSONDecodeError:
                pytest.fail(f"{key_name} is not valid JSON")
    
    def test_missing_key_file_handled(self, test_keys, monkeypatch):
        """Missing key files should be handled gracefully."""
        temp_dir = tempfile.mkdtemp(prefix="test_missing_")
        monkeypatch.setattr('vault_mcp.KEY_DIR', temp_dir)
        
        # Create only EncKey
        with open(os.path.join(temp_dir, "EncKey.json"), "w") as f:
            f.write('{"test": "key"}')
        
        result = get_public_key("DEMO-TOKEN-GET-YOUR-OWN-AT-ENVECTOR-IO")
        bundle = json.loads(result)
        
        # Should have EncKey but not others
        assert "EncKey.json" in bundle
        # Others may or may not be present (implementation dependent)
        
        shutil.rmtree(temp_dir, ignore_errors=True)
    
    def test_bundle_size_reasonable(self, test_keys, monkeypatch):
        """Bundle size should be reasonable (not empty, not too large)."""
        monkeypatch.setattr('vault_mcp.KEY_DIR', test_keys)
        
        result = get_public_key("DEMO-TOKEN-GET-YOUR-OWN-AT-ENVECTOR-IO")
        
        # Should have some content
        assert len(result) > 100
        
        # Should not be excessively large (keys are typically < 1MB each)
        assert len(result) < 10 * 1024 * 1024  # 10MB limit
    
    def test_multiple_calls_return_same_keys(self, test_keys, monkeypatch):
        """Multiple calls should return consistent keys."""
        monkeypatch.setattr('vault_mcp.KEY_DIR', test_keys)
        
        result1 = get_public_key("DEMO-TOKEN-GET-YOUR-OWN-AT-ENVECTOR-IO")
        result2 = get_public_key("DEMO-TOKEN-GET-YOUR-OWN-AT-ENVECTOR-IO")
        
        # Should be identical
        assert result1 == result2
    
    def test_different_tokens_return_same_keys(self, test_keys, monkeypatch):
        """Different valid tokens should return same keys (shared vault)."""
        monkeypatch.setattr('vault_mcp.KEY_DIR', test_keys)
        
        result1 = get_public_key("DEMO-TOKEN-GET-YOUR-OWN-AT-ENVECTOR-IO")
        result2 = get_public_key("DEMO-ADMIN-SIGNUP-AT-ENVECTOR-IO")
        
        # Keys should be identical (same vault)
        assert result1 == result2
