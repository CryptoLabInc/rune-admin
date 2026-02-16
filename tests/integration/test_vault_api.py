"""
Integration tests for Vault MCP API endpoints.
Tests the full MCP server functionality.
"""
import pytest
import sys
import os
import json
import base64
import tempfile
import shutil
import numpy as np
from httpx import AsyncClient

# Add mcp/vault to path
sys.path.insert(0, os.path.join(os.path.dirname(__file__), '../../mcp/vault'))


class TestVaultMCPAPI:
    """Integration tests for Vault MCP API."""

    @pytest.fixture(autouse=True)
    def reset_rate_limiter(self):
        """Reset rate limiter before each test."""
        import vault_mcp
        vault_mcp.rate_limiter._requests.clear()

    @pytest.fixture(scope="class")
    def vault_setup(self):
        """Setup test vault with keys."""
        temp_dir = tempfile.mkdtemp(prefix="test_vault_api_")
        
        # Generate keys
        from pyenvector.crypto import KeyGenerator
        keygen = KeyGenerator(key_path=temp_dir, key_id="test-api", dim_list=[1024])
        keygen.generate_keys()
        
        # Patch vault_mcp module
        import vault_mcp
        original_key_dir = vault_mcp.KEY_DIR
        vault_mcp.KEY_DIR = temp_dir
        vault_mcp.enc_key_path = os.path.join(temp_dir, "EncKey.json")
        vault_mcp.sec_key_path = os.path.join(temp_dir, "SecKey.json")
        
        # Reinitialize cipher
        from pyenvector.crypto import Cipher
        vault_mcp.cipher = Cipher(enc_key_path=vault_mcp.enc_key_path, dim=1024)
        
        yield {
            "key_dir": temp_dir,
            "app": vault_mcp.mcp.sse_app()
        }
        
        # Restore
        vault_mcp.KEY_DIR = original_key_dir
        shutil.rmtree(temp_dir, ignore_errors=True)
    
    @pytest.mark.asyncio
    async def test_health_endpoint(self, vault_setup):
        """Health endpoint should return 200."""
        async with AsyncClient(app=vault_setup["app"], base_url="http://test") as client:
            response = await client.get("/health")
            
            assert response.status_code == 200
    
    @pytest.mark.asyncio
    async def test_get_public_key_via_api(self, vault_setup):
        """Test get_public_key through MCP API."""
        # Note: MCP tools are called via SSE or stdio, not direct HTTP
        # This is a conceptual test - actual MCP tool invocation requires MCP client
        
        # Direct function call test
        from vault_mcp import get_public_key
        result = get_public_key("TOKEN-FOR-DEMONSTRATION-PURPOSES-ONLY-DO-NOT-USE-IN-PRODUCTION")
        
        bundle = json.loads(result)
        assert "EncKey.json" in bundle
        assert "EvalKey.json" in bundle
    
    @pytest.mark.asyncio
    async def test_decrypt_scores_via_api(self, vault_setup):
        """Test decrypt_scores through MCP API."""
        from vault_mcp import decrypt_scores, cipher
        from pyenvector.crypto.block import CipherBlock
        
        # Create encrypted scores
        scores = np.random.rand(1024).tolist()
        encrypted = cipher.encrypt(scores, encode_type="item")
        serialized = encrypted.serialize()
        blob = base64.b64encode(serialized).decode('utf-8')
        
        # Call decrypt_scores
        result = decrypt_scores("TOKEN-FOR-DEMONSTRATION-PURPOSES-ONLY-DO-NOT-USE-IN-PRODUCTION", blob, top_k=5)
        
        data = json.loads(result)
        assert isinstance(data, list)
        assert len(data) == 5
        
        for item in data:
            assert "shard_idx" in item
            assert "row_idx" in item
            assert "score" in item
    
    @pytest.mark.asyncio
    async def test_end_to_end_encrypt_decrypt(self, vault_setup):
        """Full encryption → decryption flow."""
        from vault_mcp import get_public_key, decrypt_scores, cipher
        
        # 1. Get public keys
        key_bundle = get_public_key("TOKEN-FOR-DEMONSTRATION-PURPOSES-ONLY-DO-NOT-USE-IN-PRODUCTION")
        assert key_bundle is not None
        
        # 2. Encrypt data (simulating agent) - padded to 1024
        original_scores = [0.9, 0.8, 0.7, 0.6, 0.5, 0.4, 0.3, 0.2, 0.1, 0.05] + [0.01] * 1014
        encrypted = cipher.encrypt(original_scores, encode_type="item")
        blob = base64.b64encode(encrypted.serialize()).decode('utf-8')
        
        # 3. Decrypt and get top-K
        result = decrypt_scores("TOKEN-FOR-DEMONSTRATION-PURPOSES-ONLY-DO-NOT-USE-IN-PRODUCTION", blob, top_k=3)
        data = json.loads(result)
        
        # 4. Verify top-3 are highest scores
        assert len(data) == 3
        returned_scores = [item["score"] for item in data]
        
        # Top-3 should be ~0.9, 0.8, 0.7
        assert max(returned_scores) > 0.85  # Should include 0.9
        assert min(returned_scores) > 0.65  # Should not include < 0.7
    
    def test_concurrent_decrypt_requests(self, vault_setup):
        """Multiple concurrent decrypts should work."""
        from vault_mcp import decrypt_scores, cipher
        import concurrent.futures
        
        # Create multiple encrypted blobs
        blobs = []
        for _ in range(5):
            scores = np.random.rand(1024).tolist()
            encrypted = cipher.encrypt(scores, encode_type="item")
            blob = base64.b64encode(encrypted.serialize()).decode('utf-8')
            blobs.append(blob)
        
        # Decrypt concurrently
        def decrypt_one(blob):
            return decrypt_scores("TOKEN-FOR-DEMONSTRATION-PURPOSES-ONLY-DO-NOT-USE-IN-PRODUCTION", blob, top_k=5)
        
        with concurrent.futures.ThreadPoolExecutor(max_workers=5) as executor:
            futures = [executor.submit(decrypt_one, blob) for blob in blobs]
            results = [f.result() for f in futures]
        
        # All should succeed
        for result in results:
            data = json.loads(result)
            assert isinstance(data, list)
            assert len(data) == 5
    
    def test_invalid_token_across_all_endpoints(self, vault_setup):
        """Invalid token should be rejected consistently."""
        from vault_mcp import get_public_key, decrypt_scores
        
        invalid_token = "hacker-token-123"
        
        # get_public_key should reject
        with pytest.raises(ValueError, match="Access Denied"):
            get_public_key(invalid_token)
        
        # decrypt_scores should reject
        with pytest.raises(ValueError, match="Access Denied"):
            decrypt_scores(invalid_token, "fake-blob", top_k=5)
    
    def test_rate_limiting_enforced(self, vault_setup):
        """Rate limiting (top_k <= 10) should be enforced."""
        from vault_mcp import decrypt_scores, cipher
        
        scores = np.random.rand(1024).tolist()
        encrypted = cipher.encrypt(scores, encode_type="item")
        blob = base64.b64encode(encrypted.serialize()).decode('utf-8')
        
        # top_k=10 should work
        result_10 = decrypt_scores("TOKEN-FOR-DEMONSTRATION-PURPOSES-ONLY-DO-NOT-USE-IN-PRODUCTION", blob, top_k=10)
        data_10 = json.loads(result_10)
        assert "error" not in data_10 or data_10.get("error") is None
        
        # top_k=11 should fail
        result_11 = decrypt_scores("TOKEN-FOR-DEMONSTRATION-PURPOSES-ONLY-DO-NOT-USE-IN-PRODUCTION", blob, top_k=11)
        data_11 = json.loads(result_11)
        assert "error" in data_11
        assert "Rate Limit" in data_11["error"]
