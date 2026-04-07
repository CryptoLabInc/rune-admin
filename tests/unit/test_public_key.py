"""
Unit tests for get_public_key.
"""
import pytest
import sys
import os
import json
import tempfile
import shutil

# Add vault to path
sys.path.insert(0, os.path.join(os.path.dirname(__file__), '../../vault'))

# Import the implementation function
from vault_core import _get_public_key_impl as get_public_key
import vault_core
from token_store import token_store
from pyenvector.crypto import KeyGenerator

FAKE_TEAM_SECRET = "evt_fake-team-secret-for-testing-purposes-only"


class TestGetPublicKey:

    @pytest.fixture(autouse=True)
    def reset_rate_limiter(self):
        """Reset rate limiters before each test."""
        token_store._rate_limiters.clear()

    @pytest.fixture(scope="class")
    def test_keys(self):
        """Generate test keys."""
        temp_dir = tempfile.mkdtemp(prefix="test_pubkey_")
        keygen = KeyGenerator(key_path=temp_dir, key_id="test-pubkey", dim_list=[1024], metadata_encryption=False)
        keygen.generate_keys()

        yield temp_dir
        shutil.rmtree(temp_dir, ignore_errors=True)

    @pytest.fixture(autouse=True)
    def patch_vault_paths(self, test_keys, monkeypatch):
        """Patch vault paths to point to test-generated keys."""
        monkeypatch.setattr('vault_core.KEY_SUBDIR', test_keys)
        monkeypatch.setattr('vault_core.VAULT_TEAM_SECRET', FAKE_TEAM_SECRET)

    def test_valid_token_returns_bundle(self, test_keys):
        """Valid token should return public key bundle."""
        result = get_public_key("evt_0000000000000000000000000000demo")

        # Should be valid JSON
        bundle = json.loads(result)

        # Should contain public keys
        assert "EncKey.json" in bundle
        assert "EvalKey.json" in bundle

        # Should NOT contain secret keys
        assert "SecKey.json" not in bundle
        assert "MetadataKey.json" not in bundle

    def test_invalid_token_raises_error(self, test_keys):
        """Invalid token should raise an authentication error."""
        from token_store import TokenNotFoundError
        with pytest.raises(TokenNotFoundError):
            get_public_key("invalid-token")

    def test_bundle_contains_agent_id_and_dek(self, test_keys):
        """Bundle should contain per-user agent_id and agent_dek."""
        result = get_public_key("evt_0000000000000000000000000000demo")
        bundle = json.loads(result)

        assert "agent_id" in bundle
        assert "agent_dek" in bundle
        assert len(bundle["agent_id"]) == 32  # SHA256 hex[:32]

    def test_bundle_contains_envector_credentials(self, test_keys, monkeypatch):
        """Bundle should contain enVector endpoint and API key when configured."""
        monkeypatch.setattr('vault_core.ENVECTOR_ENDPOINT', 'cluster-test.envector.io')
        monkeypatch.setattr('vault_core.ENVECTOR_API_KEY', 'test-api-key-abc123')

        result = get_public_key("evt_0000000000000000000000000000demo")
        bundle = json.loads(result)

        assert bundle["envector_endpoint"] == "cluster-test.envector.io"
        assert bundle["envector_api_key"] == "test-api-key-abc123"

    def test_bundle_envector_empty_when_not_configured(self, test_keys, monkeypatch):
        """Bundle should have null enVector fields when not configured on Vault."""
        monkeypatch.setattr('vault_core.ENVECTOR_ENDPOINT', None)
        monkeypatch.setattr('vault_core.ENVECTOR_API_KEY', None)

        result = get_public_key("evt_0000000000000000000000000000demo")
        bundle = json.loads(result)

        assert bundle.get("envector_endpoint") is None
        assert bundle.get("envector_api_key") is None
