import sys
import os
import asyncio
import numpy as np
import pickle
import base64
import json

# Adjust path to import vault_mcp
sys.path.append(os.path.dirname(os.path.abspath(__file__)))

try:
    import vault_mcp
    from pyenvector.crypto import Cipher
except ImportError as e:
    print(f"Error importing modules: {e}")
    sys.exit(1)

def run_demo():
    print("=== Mocked enVector-Vault Demo (Local) ===")
    
    # 1. Agent: Request Public Key Bundle
    print("\n[Agent] Requesting Public Key Bundle (EncKey, EvalKey, MetadataKey)...")
    # Using a valid token issued by Admin
    token = "envector-team-alpha"
    bundle_json = vault_mcp.get_public_key(token=token)
    key_bundle = json.loads(bundle_json)
    
    print(f"  Got Bundle with keys: {list(key_bundle.keys())}")
    
    # Save keys to local directory for use by encryption/cloud simulation
    # In a real Agent, this would be in memory or temp dir.
    # We use 'vault_keys' as consistent path.
    if not os.path.exists("vault_keys"):
        os.makedirs("vault_keys")
        
    for filename, content in key_bundle.items():
        with open(os.path.join("vault_keys", filename), "w") as f:
            f.write(content)
            
    # Write to tmp file for Cipher to use (since Cipher needs path)
    # Or reuse the known path for demo simplicity
    enc_key_path = "vault_keys/EncKey.json"
    
    # 2. Agent: Encrypt Query (Simulated)
    # We don't actually search, we just need to send something to "Cloud".
    print("\n[Agent] Encrypting Query...")
    # (Skipping actual query encryption as it's not relevant for this part of the demo)
    
    # 3. Cloud: Search & Score (Simulated)
    print("\n[Cloud] Processing Search... (SIMULATED)")
    dim = 32
    cloud_cipher = Cipher(enc_key_path=enc_key_path, dim=dim)
    
    # Cloud computes scores. Suppose we have 5 items.
    # Scores: Item 0 = 0.1, Item 1 = 0.95, Item 2 = 0.3, Item 3 = 0.8, Item 4 = 0.05
    # We want Vault to return top 2: Item 1 and Item 3.
    target_scores = np.zeros(dim, dtype=np.float32)
    target_scores[0] = 0.1
    target_scores[1] = 0.95
    target_scores[2] = 0.3
    target_scores[3] = 0.8
    target_scores[4] = 0.05
    
    print(f"  True Scores: {target_scores[:5]}")
    
    # Cloud "Encrypts" the scores (using Public Key)
    # NOTE: In real FHE, this is result of homomorphic op. Here we simulate by encrypting.
    print("  Encrypting scores (Simulating FHE result)...")
    enc_result = cloud_cipher.encrypt(target_scores, "item")
    
    # Serialize for transport (Native)
    # CipherBlock has serialize() method which returns bytes.
    blob_bytes = enc_result.serialize()
    blob_b64 = base64.b64encode(blob_bytes).decode('utf-8')
    print(f"  Encrypted Blob Size: {len(blob_b64)} chars")
    
    # 4. Agent: Request Decryption from Vault
    print("\n[Agent] Sending Encrypted Blob to Vault for Decryption (Top-K=3)...")
    try:
        json_result = vault_mcp.decrypt_scores(token=token, encrypted_blob_b64=blob_b64, top_k=3)
        results = json.loads(json_result)
        
        print("\n[Vault] Decrypted & Filtered Results:")
        print(json.dumps(results, indent=2))
        
        # Verify
        print("\n[Verification]")
        if len(results) == 3 and results[0]['index'] == 1 and results[1]['index'] == 3:
             print("SUCCESS: Vault correctly returned Top results!")
        else:
             print("FAILURE: Results mismatch expected top indices [1, 3, ...]")
             
    except Exception as e:
        print(f"Error calling Vault: {e}")

if __name__ == "__main__":
    run_demo()
