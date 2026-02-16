"""
Unit tests for decrypt_scores MCP tool (including Top-K).

Uses mock-based approach: since we cannot create real CiphertextScore blobs
without running FHE scoring on an actual index from enVector Cloud, we mock
cipher.decrypt_score() and CiphertextScore.ParseFromString() to test the Top-K
and response format logic.
"""
import pytest
import sys
import os
import json
import base64
import numpy as np
from unittest.mock import MagicMock, patch

# Add mcp/vault to path
sys.path.insert(0, os.path.join(os.path.dirname(__file__), '../../mcp/vault'))

# Import the implementation function (not the MCP-decorated version)
from vault_mcp import _decrypt_scores_impl as decrypt_scores, rate_limiter


def _make_fake_blob() -> str:
    """Create a fake base64-encoded blob (content doesn't matter since decrypt_score is mocked)."""
    return base64.b64encode(b"fake_ciphertext_score_proto").decode("utf-8")


def _mock_decrypt_score_flat(scores):
    """Build a mock decrypt_score return value for FLAT index (single shard)."""
    return {"score": [scores], "shard_idx": [0]}


def _mock_decrypt_score_ivf(score_2d, shard_indices):
    """Build a mock decrypt_score return value for IVF_FLAT index (multiple shards)."""
    return {"score": score_2d, "shard_idx": shard_indices}


class TestDecryptScores:

    @pytest.fixture(autouse=True)
    def reset_rate_limiter(self):
        """Reset rate limiter before each test."""
        rate_limiter._requests.clear()

    def _patch_cipher_and_proto(self, monkeypatch, scores_return):
        """Helper to mock cipher, CiphertextScore, and CipherBlock."""
        mock_cipher = MagicMock()
        mock_cipher.decrypt_score.return_value = scores_return
        monkeypatch.setattr('vault_mcp.cipher', mock_cipher)
        monkeypatch.setattr('vault_mcp.sec_key_path', '/fake/SecKey.json')
        monkeypatch.setattr('vault_mcp.CiphertextScore', MagicMock)
        monkeypatch.setattr('vault_mcp.CipherBlock', MagicMock)

    def test_decrypt_valid_scores_flat(self, monkeypatch):
        """Valid encrypted scores (FLAT) should decrypt successfully."""
        scores = np.random.rand(100).tolist()
        blob = _make_fake_blob()

        self._patch_cipher_and_proto(monkeypatch, _mock_decrypt_score_flat(scores))

        result = decrypt_scores("TOKEN-FOR-DEMONSTRATION-PURPOSES-ONLY-DO-NOT-USE-IN-PRODUCTION", blob, top_k=5)
        data = json.loads(result)

        assert isinstance(data, list)
        assert len(data) == 5
        for item in data:
            assert "shard_idx" in item
            assert "row_idx" in item
            assert "score" in item

    def test_decrypt_valid_scores_ivf(self, monkeypatch):
        """Valid encrypted scores (IVF_FLAT) should decrypt successfully with shard mapping."""
        shard0 = [0.1, 0.9, 0.3]
        shard1 = [0.8, 0.2, 0.7]
        blob = _make_fake_blob()

        self._patch_cipher_and_proto(monkeypatch, _mock_decrypt_score_ivf(
            [shard0, shard1], [5, 12]
        ))

        result = decrypt_scores("TOKEN-FOR-DEMONSTRATION-PURPOSES-ONLY-DO-NOT-USE-IN-PRODUCTION", blob, top_k=3)
        data = json.loads(result)

        assert isinstance(data, list)
        assert len(data) == 3
        # Top 3: shard 5 row 1 (0.9), shard 12 row 0 (0.8), shard 12 row 2 (0.7)
        assert data[0] == {"shard_idx": 5, "row_idx": 1, "score": 0.9}
        assert data[1] == {"shard_idx": 12, "row_idx": 0, "score": 0.8}
        assert data[2] == {"shard_idx": 12, "row_idx": 2, "score": 0.7}

    def test_top_k_returns_correct_count(self, monkeypatch):
        """Top-K should return exactly K results."""
        scores = np.random.rand(50).tolist()
        blob = _make_fake_blob()

        self._patch_cipher_and_proto(monkeypatch, _mock_decrypt_score_flat(scores))

        for k in [1, 2, 3, 5]:
            result = decrypt_scores("TOKEN-FOR-DEMONSTRATION-PURPOSES-ONLY-DO-NOT-USE-IN-PRODUCTION", blob, top_k=k)
            data = json.loads(result)
            assert isinstance(data, list)
            assert len(data) == k, f"Expected {k} results, got {len(data)}"

    def test_top_k_returns_highest_scores(self, monkeypatch):
        """Top-K should return the highest scoring items."""
        scores = [0.1, 0.9, 0.3, 0.8, 0.5]
        blob = _make_fake_blob()

        self._patch_cipher_and_proto(monkeypatch, _mock_decrypt_score_flat(scores))

        result = decrypt_scores("TOKEN-FOR-DEMONSTRATION-PURPOSES-ONLY-DO-NOT-USE-IN-PRODUCTION", blob, top_k=2)
        data = json.loads(result)

        assert isinstance(data, list)
        assert len(data) == 2
        returned_scores = [item["score"] for item in data]
        assert returned_scores[0] == pytest.approx(0.9)
        assert returned_scores[1] == pytest.approx(0.8)

    def test_top_k_limit_enforced(self):
        """Top-K > 10 should be rejected (policy)."""
        blob = _make_fake_blob()

        result = decrypt_scores("TOKEN-FOR-DEMONSTRATION-PURPOSES-ONLY-DO-NOT-USE-IN-PRODUCTION", blob, top_k=15)
        data = json.loads(result)

        assert "error" in data
        assert "Rate Limit" in data["error"] or "Max top_k" in data["error"]

    def test_invalid_token_rejected(self):
        """Invalid token should raise ValueError."""
        blob = _make_fake_blob()

        with pytest.raises(ValueError, match="Access Denied"):
            decrypt_scores("invalid-token", blob, top_k=5)

    def test_malformed_blob_returns_error(self):
        """Malformed encrypted blob should return error."""

        result = decrypt_scores("TOKEN-FOR-DEMONSTRATION-PURPOSES-ONLY-DO-NOT-USE-IN-PRODUCTION", "not-valid-base64!!!", top_k=5)
        data = json.loads(result)

        assert "error" in data

    def test_empty_blob_returns_empty_or_error(self):
        """Empty blob should return error or empty result list."""
        result = decrypt_scores("TOKEN-FOR-DEMONSTRATION-PURPOSES-ONLY-DO-NOT-USE-IN-PRODUCTION", "", top_k=5)
        data = json.loads(result)

        # Empty base64 decodes to b"", which produces an empty protobuf
        # with no scores — either an error dict or an empty list is acceptable
        assert isinstance(data, (dict, list))

    def test_result_format_correct(self, monkeypatch):
        """Result should have correct format: [{shard_idx, row_idx, score}, ...]."""
        scores = np.random.rand(20).tolist()
        blob = _make_fake_blob()

        self._patch_cipher_and_proto(monkeypatch, _mock_decrypt_score_flat(scores))

        result = decrypt_scores("TOKEN-FOR-DEMONSTRATION-PURPOSES-ONLY-DO-NOT-USE-IN-PRODUCTION", blob, top_k=5)
        data = json.loads(result)

        assert isinstance(data, list)
        for item in data:
            assert "shard_idx" in item
            assert "row_idx" in item
            assert "score" in item
            assert isinstance(item["shard_idx"], int)
            assert isinstance(item["row_idx"], int)
            assert isinstance(item["score"], (int, float))

    def test_scores_sorted_descending(self, monkeypatch):
        """Returned scores should be sorted in descending order."""
        scores = np.random.rand(30).tolist()
        blob = _make_fake_blob()

        self._patch_cipher_and_proto(monkeypatch, _mock_decrypt_score_flat(scores))

        result = decrypt_scores("TOKEN-FOR-DEMONSTRATION-PURPOSES-ONLY-DO-NOT-USE-IN-PRODUCTION", blob, top_k=5)
        data = json.loads(result)

        assert isinstance(data, list) and len(data) > 1
        returned_scores = [item["score"] for item in data]
        for i in range(len(returned_scores) - 1):
            assert returned_scores[i] >= returned_scores[i + 1], "Scores not sorted descending"

    def test_default_top_k_is_5(self, monkeypatch):
        """Default top_k should be 5."""
        scores = np.random.rand(50).tolist()
        blob = _make_fake_blob()

        self._patch_cipher_and_proto(monkeypatch, _mock_decrypt_score_flat(scores))

        result = decrypt_scores("TOKEN-FOR-DEMONSTRATION-PURPOSES-ONLY-DO-NOT-USE-IN-PRODUCTION", blob)
        data = json.loads(result)

        assert isinstance(data, list)
        assert len(data) == 5, "Default top_k should be 5"

    def test_ivf_topk_cross_shard(self, monkeypatch):
        """Top-K across multiple IVF shards should pick globally highest scores."""
        shard0 = [0.1, 0.5, 0.3]
        shard1 = [0.9, 0.2, 0.8]
        shard2 = [0.4, 0.6, 0.7]
        blob = _make_fake_blob()

        self._patch_cipher_and_proto(monkeypatch, _mock_decrypt_score_ivf(
            [shard0, shard1, shard2], [10, 20, 30]
        ))

        result = decrypt_scores("TOKEN-FOR-DEMONSTRATION-PURPOSES-ONLY-DO-NOT-USE-IN-PRODUCTION", blob, top_k=4)
        data = json.loads(result)

        assert len(data) == 4
        # Expected top-4: shard20 row0 (0.9), shard20 row2 (0.8), shard30 row2 (0.7), shard30 row1 (0.6)
        assert data[0] == {"shard_idx": 20, "row_idx": 0, "score": 0.9}
        assert data[1] == {"shard_idx": 20, "row_idx": 2, "score": 0.8}
        assert data[2] == {"shard_idx": 30, "row_idx": 2, "score": 0.7}
        assert data[3] == {"shard_idx": 30, "row_idx": 1, "score": 0.6}
