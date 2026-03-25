"""
Unit tests for get_public_key.
"""
import pytest
import sys
import os
import json
import tempfile
import shutil
from pathlib import Path

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
        result = get_public_key("TOKEN-FOR-DEMONSTRATION-PURPOSES-ONLY-DO-NOT-USE-IN-PRODUCTION")

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

    def test_returned_keys_are_valid_json(self, test_keys):
        """Each key file in bundle should be valid JSON."""
        result = get_public_key("TOKEN-FOR-DEMONSTRATION-PURPOSES-ONLY-DO-NOT-USE-IN-PRODUCTION")
        bundle = json.loads(result)

        # Only validate actual key file fields; skip non-file fields
        key_file_fields = {"EncKey.json", "EvalKey.json"}
        for key_name in key_file_fields:
            if key_name not in bundle:
                continue
            try:
                json.loads(bundle[key_name])
            except json.JSONDecodeError:
                pytest.fail(f"{key_name} is not valid JSON")

    def test_missing_key_file_handled(self, test_keys, monkeypatch):
        """Missing key files should be handled gracefully."""
        temp_dir = tempfile.mkdtemp(prefix="test_missing_")
        monkeypatch.setattr('vault_core.KEY_SUBDIR', temp_dir)

        # Create only EncKey
        with open(os.path.join(temp_dir, "EncKey.json"), "w") as f:
            f.write('{"test": "key"}')

        result = get_public_key("TOKEN-FOR-DEMONSTRATION-PURPOSES-ONLY-DO-NOT-USE-IN-PRODUCTION")
        bundle = json.loads(result)

        # Should have EncKey but not others
        assert "EncKey.json" in bundle
        # Others may or may not be present (implementation dependent)

        shutil.rmtree(temp_dir, ignore_errors=True)

    def test_bundle_size_reasonable(self, test_keys):
        """Bundle size should be reasonable (not empty, not too large)."""
        result = get_public_key("TOKEN-FOR-DEMONSTRATION-PURPOSES-ONLY-DO-NOT-USE-IN-PRODUCTION")

        # Should have some content
        assert len(result) > 100

        # Should not be excessively large (keys are typically < 1MB each)
        assert len(result) < 10 * 1024 * 1024  # 10MB limit

    def test_multiple_calls_return_same_keys(self, test_keys):
        """Multiple calls should return consistent keys."""
        result1 = get_public_key("TOKEN-FOR-DEMONSTRATION-PURPOSES-ONLY-DO-NOT-USE-IN-PRODUCTION")
        result2 = get_public_key("TOKEN-FOR-DEMONSTRATION-PURPOSES-ONLY-DO-NOT-USE-IN-PRODUCTION")

        # Should be identical
        assert result1 == result2

    def test_bundle_contains_agent_id_and_dek(self, test_keys):
        """Bundle should contain per-user agent_id and agent_dek."""
        result = get_public_key("TOKEN-FOR-DEMONSTRATION-PURPOSES-ONLY-DO-NOT-USE-IN-PRODUCTION")
        bundle = json.loads(result)

        assert "agent_id" in bundle
        assert "agent_dek" in bundle
        assert len(bundle["agent_id"]) == 32  # SHA256 hex[:32]
