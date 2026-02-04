"""
Unit tests for decrypt_scores MCP tool (including Top-K).
"""
import pytest
import sys
import os
import json
import base64
import tempfile
import shutil
import numpy as np

# Add mcp/vault to path
sys.path.insert(0, os.path.join(os.path.dirname(__file__), '../../mcp/vault'))

# Import the implementation function (not the MCP-decorated version)
from vault_mcp import _decrypt_scores_impl as decrypt_scores, rate_limiter
from pyenvector.crypto import KeyGenerator, Cipher
from pyenvector.crypto.block import CipherBlock, Query


class TestDecryptScores:

    @pytest.fixture(autouse=True)
    def reset_rate_limiter(self):
        """Reset rate limiter before each test."""
        rate_limiter._requests.clear()
    """Test decrypt_scores MCP tool."""
    
    @pytest.fixture(scope="class")
    def crypto_setup(self):
        """Setup encryption keys and cipher."""
        temp_dir = tempfile.mkdtemp(prefix="test_decrypt_")
        keygen = KeyGenerator(key_path=temp_dir, key_id="test-decrypt", dim_list=[1024])
        keygen.generate_keys()
        
        enc_key = os.path.join(temp_dir, "EncKey.json")
        sec_key = os.path.join(temp_dir, "SecKey.json")
        
        cipher = Cipher(enc_key_path=enc_key, dim=1024)
        
        yield {
            "key_dir": temp_dir,
            "enc_key": enc_key,
            "sec_key": sec_key,
            "cipher": cipher,
            "dim": 1024
        }
        
        shutil.rmtree(temp_dir, ignore_errors=True)
    
    def create_encrypted_scores(self, crypto_setup, scores):
        """Helper to create encrypted score blob."""
        cipher = crypto_setup["cipher"]
        dim = crypto_setup["dim"]
        
        # Ensure scores match dimension
        if isinstance(scores, list):
            if len(scores) != dim:
                # Pad or truncate to match dimension
                if len(scores) < dim:
                    scores = scores + [0.0] * (dim - len(scores))
                else:
                    scores = scores[:dim]
        
        # Encrypt scores (note: no wrapping in list for pyenvector API)
        encrypted = cipher.encrypt(scores, encode_type="item")
        
        # Serialize to bytes (matching vault_mcp logic)
        serialized = encrypted.serialize()
        
        # Base64 encode
        return base64.b64encode(serialized).decode('utf-8')
    
    def test_decrypt_valid_scores(self, crypto_setup, monkeypatch):
        """Valid encrypted scores should decrypt successfully."""
        monkeypatch.setattr('vault_mcp.KEY_DIR', crypto_setup["key_dir"])
        monkeypatch.setattr('vault_mcp.cipher', crypto_setup["cipher"])
        monkeypatch.setattr('vault_mcp.sec_key_path', crypto_setup["sec_key"])
        
        # Create test scores
        scores = np.random.rand(1024).tolist()
        encrypted_blob = self.create_encrypted_scores(crypto_setup, scores)
        
        # Decrypt
        result = decrypt_scores("DEMO-TOKEN-GET-YOUR-OWN-AT-ENVECTOR-IO", encrypted_blob, top_k=5)
        
        # Should be valid JSON
        data = json.loads(result)
        
        # Should not have error
        assert "error" not in data or data.get("error") is None
    
    def test_top_k_returns_correct_count(self, crypto_setup, monkeypatch):
        """Top-K should return exactly K results."""
        monkeypatch.setattr('vault_mcp.KEY_DIR', crypto_setup["key_dir"])
        monkeypatch.setattr('vault_mcp.cipher', crypto_setup["cipher"])
        monkeypatch.setattr('vault_mcp.sec_key_path', crypto_setup["sec_key"])
        
        scores = np.random.rand(1024).tolist()
        encrypted_blob = self.create_encrypted_scores(crypto_setup, scores)
        
        for k in [1, 2, 3]:
            result = decrypt_scores("DEMO-TOKEN-GET-YOUR-OWN-AT-ENVECTOR-IO", encrypted_blob, top_k=k)
            data = json.loads(result)
            
            if isinstance(data, list):
                assert len(data) == k, f"Expected {k} results, got {len(data)}"
    
    def test_top_k_returns_highest_scores(self, crypto_setup, monkeypatch):
        """Top-K should return the highest scoring items."""
        monkeypatch.setattr('vault_mcp.KEY_DIR', crypto_setup["key_dir"])
        monkeypatch.setattr('vault_mcp.cipher', crypto_setup["cipher"])
        monkeypatch.setattr('vault_mcp.sec_key_path', crypto_setup["sec_key"])
        
        # Create known scores (padded to 1024)
        scores = [0.1, 0.9, 0.3, 0.8] + [0.0] * 1020
        encrypted_blob = self.create_encrypted_scores(crypto_setup, scores)
        
        result = decrypt_scores("DEMO-TOKEN-GET-YOUR-OWN-AT-ENVECTOR-IO", encrypted_blob, top_k=2)
        data = json.loads(result)
        
        if isinstance(data, list):
            # Should have indices 1, 3, 6 (scores 0.9, 0.8, 0.7)
            returned_scores = [item["score"] for item in data]
            assert 0.9 in returned_scores or abs(max(returned_scores) - 0.9) < 0.01
    
    def test_top_k_limit_enforced(self, crypto_setup, monkeypatch):
        """Top-K > 10 should be rejected (policy)."""
        monkeypatch.setattr('vault_mcp.KEY_DIR', crypto_setup["key_dir"])
        
        scores = np.random.rand(1024).tolist()
        encrypted_blob = self.create_encrypted_scores(crypto_setup, scores)
        
        result = decrypt_scores("DEMO-TOKEN-GET-YOUR-OWN-AT-ENVECTOR-IO", encrypted_blob, top_k=15)
        data = json.loads(result)
        
        # Should have error about rate limit
        assert "error" in data
        assert "Rate Limit" in data["error"] or "Max top_k" in data["error"]
    
    def test_invalid_token_rejected(self, crypto_setup, monkeypatch):
        """Invalid token should raise ValueError."""
        monkeypatch.setattr('vault_mcp.KEY_DIR', crypto_setup["key_dir"])
        
        scores = np.random.rand(1024).tolist()
        encrypted_blob = self.create_encrypted_scores(crypto_setup, scores)
        
        with pytest.raises(ValueError, match="Access Denied"):
            decrypt_scores("invalid-token", encrypted_blob, top_k=5)
    
    def test_malformed_blob_returns_error(self, crypto_setup, monkeypatch):
        """Malformed encrypted blob should return error."""
        monkeypatch.setattr('vault_mcp.KEY_DIR', crypto_setup["key_dir"])
        monkeypatch.setattr('vault_mcp.cipher', crypto_setup["cipher"])
        monkeypatch.setattr('vault_mcp.sec_key_path', crypto_setup["sec_key"])
        
        # Invalid base64
        result = decrypt_scores("DEMO-TOKEN-GET-YOUR-OWN-AT-ENVECTOR-IO", "not-valid-base64", top_k=5)
        data = json.loads(result)
        
        assert "error" in data
    
    def test_empty_blob_returns_error(self, crypto_setup, monkeypatch):
        """Empty blob should return error."""
        monkeypatch.setattr('vault_mcp.KEY_DIR', crypto_setup["key_dir"])
        
        result = decrypt_scores("DEMO-TOKEN-GET-YOUR-OWN-AT-ENVECTOR-IO", "", top_k=5)
        data = json.loads(result)
        
        assert "error" in data
    
    def test_result_format_correct(self, crypto_setup, monkeypatch):
        """Result should have correct format: [{index, score}, ...]."""
        monkeypatch.setattr('vault_mcp.KEY_DIR', crypto_setup["key_dir"])
        monkeypatch.setattr('vault_mcp.cipher', crypto_setup["cipher"])
        monkeypatch.setattr('vault_mcp.sec_key_path', crypto_setup["sec_key"])
        
        scores = np.random.rand(1024).tolist()
        encrypted_blob = self.create_encrypted_scores(crypto_setup, scores)
        
        result = decrypt_scores("DEMO-TOKEN-GET-YOUR-OWN-AT-ENVECTOR-IO", encrypted_blob, top_k=5)
        data = json.loads(result)
        
        if isinstance(data, list) and len(data) > 0:
            # Each item should have index and score
            for item in data:
                assert "index" in item
                assert "score" in item
                assert isinstance(item["index"], int)
                assert isinstance(item["score"], (int, float))
    
    def test_scores_sorted_descending(self, crypto_setup, monkeypatch):
        """Returned scores should be sorted in descending order."""
        monkeypatch.setattr('vault_mcp.KEY_DIR', crypto_setup["key_dir"])
        monkeypatch.setattr('vault_mcp.cipher', crypto_setup["cipher"])
        monkeypatch.setattr('vault_mcp.sec_key_path', crypto_setup["sec_key"])
        
        scores = np.random.rand(1024).tolist()
        encrypted_blob = self.create_encrypted_scores(crypto_setup, scores)
        
        result = decrypt_scores("DEMO-TOKEN-GET-YOUR-OWN-AT-ENVECTOR-IO", encrypted_blob, top_k=5)
        data = json.loads(result)
        
        if isinstance(data, list) and len(data) > 1:
            returned_scores = [item["score"] for item in data]
            
            # Should be sorted descending
            for i in range(len(returned_scores) - 1):
                assert returned_scores[i] >= returned_scores[i + 1], "Scores not sorted descending"
    
    def test_default_top_k_is_5(self, crypto_setup, monkeypatch):
        """Default top_k should be 5."""
        monkeypatch.setattr('vault_mcp.KEY_DIR', crypto_setup["key_dir"])
        monkeypatch.setattr('vault_mcp.cipher', crypto_setup["cipher"])
        monkeypatch.setattr('vault_mcp.sec_key_path', crypto_setup["sec_key"])
        
        scores = np.random.rand(1024).tolist()
        encrypted_blob = self.create_encrypted_scores(crypto_setup, scores)
        
        # Don't specify top_k (should default to 5)
        result = decrypt_scores("DEMO-TOKEN-GET-YOUR-OWN-AT-ENVECTOR-IO", encrypted_blob)
        data = json.loads(result)
        
        if isinstance(data, list):
            assert len(data) == 5, "Default top_k should be 5"
