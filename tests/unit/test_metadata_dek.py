"""
Unit tests for per-agent metadata DEK derivation and decrypt_metadata.

Uses mock-based approach: aes_decrypt_metadata is mocked to test the
envelope parsing and per-agent HKDF key derivation without requiring
real FHE keys.
"""
import pytest
import sys
import os
import json
from unittest.mock import MagicMock

# Add vault to path
sys.path.insert(0, os.path.join(os.path.dirname(__file__), '../../vault'))

from vault_core import (
    derive_agent_key,
    _decrypt_metadata_impl as decrypt_metadata,
)
import vault_core
from token_store import token_store

VALID_TOKEN = "TOKEN-FOR-DEMONSTRATION-PURPOSES-ONLY-DO-NOT-USE-IN-PRODUCTION"
FAKE_TEAM_SECRET = "evt_fake-team-secret-for-testing-purposes-only"


# =============================================================================
# derive_agent_key tests
# =============================================================================
class TestDeriveAgentKey:

    def test_deterministic(self):
        """Same inputs must produce the same DEK."""
        dek1 = derive_agent_key("my-team-secret", "agent-abc")
        dek2 = derive_agent_key("my-team-secret", "agent-abc")
        assert dek1 == dek2

    def test_different_agent_id_different_dek(self):
        """Different agent_id must produce different DEKs."""
        dek_a = derive_agent_key("my-team-secret", "agent-aaa")
        dek_b = derive_agent_key("my-team-secret", "agent-bbb")
        assert dek_a != dek_b

    def test_output_is_32_bytes(self):
        """DEK must be exactly 32 bytes (AES-256)."""
        dek = derive_agent_key("some-secret", "any-agent")
        assert isinstance(dek, bytes)
        assert len(dek) == 32

    def test_different_team_secret_different_dek(self):
        """Different team secrets must produce different DEKs for the same agent."""
        dek1 = derive_agent_key("secret-1", "agent-x")
        dek2 = derive_agent_key("secret-2", "agent-x")
        assert dek1 != dek2


# =============================================================================
# _decrypt_metadata_impl tests
# =============================================================================
class TestDecryptMetadataImpl:

    @pytest.fixture(autouse=True)
    def reset_state(self):
        """Reset rate limiters before each test."""
        token_store._rate_limiters.clear()

    def _make_envelope(self, agent_id: str, ciphertext_b64: str) -> str:
        """Build a JSON envelope string."""
        return json.dumps({"a": agent_id, "c": ciphertext_b64})

    def test_per_agent_envelope_decryption(self, monkeypatch):
        """Per-agent JSON envelope should be parsed and decrypted with derived DEK."""
        monkeypatch.setattr('vault_core.VAULT_TEAM_SECRET', FAKE_TEAM_SECRET)

        expected_dek = derive_agent_key(FAKE_TEAM_SECRET, "agent123")
        mock_decrypt = MagicMock(return_value=b'{"text": "hello"}')
        monkeypatch.setattr('vault_core.aes_decrypt_metadata', mock_decrypt)

        envelope = self._make_envelope("agent123", "Y2lwaGVydGV4dA==")
        result = decrypt_metadata(VALID_TOKEN, [envelope])
        data = json.loads(result)

        assert isinstance(data, list)
        assert len(data) == 1
        assert data[0] == '{"text": "hello"}'

        # Verify aes_decrypt_metadata was called with the ciphertext and derived DEK
        mock_decrypt.assert_called_once_with("Y2lwaGVydGV4dA==", expected_dek)

    def test_missing_team_secret_returns_error(self, monkeypatch):
        """Missing VAULT_TEAM_SECRET should return error."""
        monkeypatch.setattr('vault_core.VAULT_TEAM_SECRET', '')

        result = decrypt_metadata(VALID_TOKEN, ["anything"])
        data = json.loads(result)

        assert "error" in data
        assert "VAULT_TEAM_SECRET not configured" in data["error"]

    def test_invalid_token_rejected(self):
        """Invalid token should raise an authentication error."""
        from token_store import TokenNotFoundError
        with pytest.raises(TokenNotFoundError):
            decrypt_metadata("bad-token", ["anything"])

    def test_invalid_envelope_returns_error(self, monkeypatch):
        """Non-JSON envelope should return a decryption error."""
        monkeypatch.setattr('vault_core.VAULT_TEAM_SECRET', FAKE_TEAM_SECRET)

        result = decrypt_metadata(VALID_TOKEN, ["not-valid-json"])
        data = json.loads(result)

        assert "error" in data

    def test_missing_key_in_envelope_returns_error(self, monkeypatch):
        """JSON without 'a' or 'c' key should return a decryption error."""
        monkeypatch.setattr('vault_core.VAULT_TEAM_SECRET', FAKE_TEAM_SECRET)

        bad_envelope = json.dumps({"x": "y"})
        result = decrypt_metadata(VALID_TOKEN, [bad_envelope])
        data = json.loads(result)

        assert "error" in data

    def test_decryption_error_returns_error(self, monkeypatch):
        """If aes_decrypt_metadata raises, should return error JSON."""
        monkeypatch.setattr('vault_core.VAULT_TEAM_SECRET', FAKE_TEAM_SECRET)

        mock_decrypt = MagicMock(side_effect=Exception("decrypt boom"))
        monkeypatch.setattr('vault_core.aes_decrypt_metadata', mock_decrypt)

        envelope = self._make_envelope("agent1", "ct_data")
        result = decrypt_metadata(VALID_TOKEN, [envelope])
        data = json.loads(result)

        assert "error" in data
        assert "decrypt boom" in data["error"]

    def test_multiple_envelopes(self, monkeypatch):
        """Multiple envelopes with different agent_ids should each derive correct DEK."""
        monkeypatch.setattr('vault_core.VAULT_TEAM_SECRET', FAKE_TEAM_SECRET)

        dek_a = derive_agent_key(FAKE_TEAM_SECRET, "agentA")
        dek_b = derive_agent_key(FAKE_TEAM_SECRET, "agentB")

        def side_effect(ct, key):
            if key == dek_a:
                return b'result-a'
            if key == dek_b:
                return b'result-b'
            return b'unknown'

        mock_decrypt = MagicMock(side_effect=side_effect)
        monkeypatch.setattr('vault_core.aes_decrypt_metadata', mock_decrypt)

        env_a = self._make_envelope("agentA", "ct_a")
        env_b = self._make_envelope("agentB", "ct_b")
        result = decrypt_metadata(VALID_TOKEN, [env_a, env_b])
        data = json.loads(result)

        assert len(data) == 2
        assert data[0] == "result-a"
        assert data[1] == "result-b"
