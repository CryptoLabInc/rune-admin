"""
Unit tests for per-agent metadata DEK derivation and decrypt_metadata.

Uses mock-based approach: aes_decrypt_metadata is mocked to test the
envelope parsing, per-agent key derivation, and legacy fallback logic
without requiring real FHE keys.
"""
import pytest
import sys
import os
import json
import base64
from unittest.mock import MagicMock, patch, call

# Add mcp/vault to path
sys.path.insert(0, os.path.join(os.path.dirname(__file__), '../../mcp/vault'))

from vault_mcp import (
    derive_agent_key,
    _decrypt_metadata_impl as decrypt_metadata,
    _load_master_key,
    rate_limiter,
)
import vault_mcp

VALID_TOKEN = "TOKEN-FOR-DEMONSTRATION-PURPOSES-ONLY-DO-NOT-USE-IN-PRODUCTION"


# =============================================================================
# derive_agent_key tests
# =============================================================================
class TestDeriveAgentKey:

    def test_deterministic(self):
        """Same inputs must produce the same DEK."""
        master = b"master-key-bytes-for-testing-1234"
        dek1 = derive_agent_key(master, "agent-abc")
        dek2 = derive_agent_key(master, "agent-abc")
        assert dek1 == dek2

    def test_different_agent_id_different_dek(self):
        """Different agent_id must produce different DEKs."""
        master = b"master-key-bytes-for-testing-1234"
        dek_a = derive_agent_key(master, "agent-aaa")
        dek_b = derive_agent_key(master, "agent-bbb")
        assert dek_a != dek_b

    def test_output_is_32_bytes(self):
        """DEK must be exactly 32 bytes (AES-256)."""
        master = b"some-master-key"
        dek = derive_agent_key(master, "any-agent")
        assert isinstance(dek, bytes)
        assert len(dek) == 32

    def test_different_master_key_different_dek(self):
        """Different master keys must produce different DEKs for the same agent."""
        dek1 = derive_agent_key(b"master-key-1", "agent-x")
        dek2 = derive_agent_key(b"master-key-2", "agent-x")
        assert dek1 != dek2


# =============================================================================
# _decrypt_metadata_impl tests
# =============================================================================
class TestDecryptMetadataImpl:

    @pytest.fixture(autouse=True)
    def reset_state(self):
        """Reset rate limiter and master key cache before each test."""
        rate_limiter._requests.clear()
        _load_master_key.cache_clear()

    def _make_envelope(self, agent_id: str, ciphertext_b64: str) -> str:
        """Build a JSON envelope string."""
        return json.dumps({"a": agent_id, "c": ciphertext_b64})

    def test_per_agent_envelope_decryption(self, monkeypatch):
        """Per-agent JSON envelope should be parsed and decrypted with derived DEK."""
        fake_master = b"fake-master-key-32-bytes-long!!!"
        monkeypatch.setattr('vault_mcp._load_master_key', lambda: fake_master)
        monkeypatch.setattr('vault_mcp.metadata_key_path', '/fake/MetadataKey.json')
        # Make metadata_key_path exist check pass
        monkeypatch.setattr('os.path.exists', lambda p: True)

        expected_dek = derive_agent_key(fake_master, "agent123")
        mock_decrypt = MagicMock(return_value=b'{"text": "hello"}')
        monkeypatch.setattr('vault_mcp.aes_decrypt_metadata', mock_decrypt)

        envelope = self._make_envelope("agent123", "Y2lwaGVydGV4dA==")
        result = decrypt_metadata(VALID_TOKEN, [envelope])
        data = json.loads(result)

        assert isinstance(data, list)
        assert len(data) == 1
        assert data[0] == '{"text": "hello"}'

        # Verify aes_decrypt_metadata was called with the ciphertext and derived DEK
        mock_decrypt.assert_called_once_with("Y2lwaGVydGV4dA==", expected_dek)

    def test_legacy_fallback(self, monkeypatch):
        """Plain base64 (non-JSON) should fall back to master metadata key."""
        fake_master = b"fake-master-key-32-bytes-long!!!"
        monkeypatch.setattr('vault_mcp._load_master_key', lambda: fake_master)
        monkeypatch.setattr('vault_mcp.metadata_key_path', '/fake/MetadataKey.json')
        monkeypatch.setattr('os.path.exists', lambda p: True)

        mock_decrypt = MagicMock(return_value=b'legacy plaintext')
        monkeypatch.setattr('vault_mcp.aes_decrypt_metadata', mock_decrypt)

        # Plain base64 string — not valid JSON envelope
        legacy_blob = base64.b64encode(b"some-encrypted-data").decode()
        result = decrypt_metadata(VALID_TOKEN, [legacy_blob])
        data = json.loads(result)

        assert isinstance(data, list)
        assert len(data) == 1
        assert data[0] == "legacy plaintext"

        # Should have been called with the raw blob and the metadata_key_path
        mock_decrypt.assert_called_once_with(legacy_blob, '/fake/MetadataKey.json')

    def test_missing_key_in_envelope_triggers_fallback(self, monkeypatch):
        """JSON without 'a' or 'c' key should fall back to legacy path."""
        fake_master = b"fake-master-key-32-bytes-long!!!"
        monkeypatch.setattr('vault_mcp._load_master_key', lambda: fake_master)
        monkeypatch.setattr('vault_mcp.metadata_key_path', '/fake/MetadataKey.json')
        monkeypatch.setattr('os.path.exists', lambda p: True)

        mock_decrypt = MagicMock(return_value=b'fallback result')
        monkeypatch.setattr('vault_mcp.aes_decrypt_metadata', mock_decrypt)

        # Valid JSON but missing expected keys
        bad_envelope = json.dumps({"x": "y"})
        result = decrypt_metadata(VALID_TOKEN, [bad_envelope])
        data = json.loads(result)

        assert data[0] == "fallback result"
        mock_decrypt.assert_called_once_with(bad_envelope, '/fake/MetadataKey.json')

    def test_mixed_envelope_and_legacy(self, monkeypatch):
        """A list mixing per-agent envelopes and legacy blobs should handle both."""
        fake_master = b"fake-master-key-32-bytes-long!!!"
        monkeypatch.setattr('vault_mcp._load_master_key', lambda: fake_master)
        monkeypatch.setattr('vault_mcp.metadata_key_path', '/fake/MetadataKey.json')
        monkeypatch.setattr('os.path.exists', lambda p: True)

        expected_dek = derive_agent_key(fake_master, "agentABC")

        def side_effect(ct, key):
            if isinstance(key, bytes):
                return b'per-agent result'
            return b'legacy result'

        mock_decrypt = MagicMock(side_effect=side_effect)
        monkeypatch.setattr('vault_mcp.aes_decrypt_metadata', mock_decrypt)

        envelope = self._make_envelope("agentABC", "ZW5jcnlwdGVk")
        legacy_blob = "cGxhaW4tYjY0"  # not valid JSON

        result = decrypt_metadata(VALID_TOKEN, [envelope, legacy_blob])
        data = json.loads(result)

        assert len(data) == 2
        assert data[0] == "per-agent result"
        assert data[1] == "legacy result"

    def test_metadata_key_not_found(self, monkeypatch):
        """Missing MetadataKey file should return error."""
        monkeypatch.setattr('vault_mcp.metadata_key_path', '/nonexistent/MetadataKey.json')
        _load_master_key.cache_clear()

        result = decrypt_metadata(VALID_TOKEN, ["anything"])
        data = json.loads(result)

        assert "error" in data
        assert "MetadataKey not found" in data["error"]

    def test_invalid_token_rejected(self):
        """Invalid token should raise ValueError."""
        with pytest.raises(ValueError, match="Access Denied"):
            decrypt_metadata("bad-token", ["anything"])

    def test_decryption_error_returns_error(self, monkeypatch):
        """If aes_decrypt_metadata raises, should return error JSON."""
        fake_master = b"fake-master-key-32-bytes-long!!!"
        monkeypatch.setattr('vault_mcp._load_master_key', lambda: fake_master)
        monkeypatch.setattr('vault_mcp.metadata_key_path', '/fake/MetadataKey.json')
        monkeypatch.setattr('os.path.exists', lambda p: True)

        mock_decrypt = MagicMock(side_effect=Exception("decrypt boom"))
        monkeypatch.setattr('vault_mcp.aes_decrypt_metadata', mock_decrypt)

        envelope = self._make_envelope("agent1", "ct_data")
        result = decrypt_metadata(VALID_TOKEN, [envelope])
        data = json.loads(result)

        assert "error" in data
        assert "decrypt boom" in data["error"]
