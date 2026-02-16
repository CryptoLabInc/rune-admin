import os
import shutil
import numpy as np
from pyenvector.crypto import KeyGenerator, Cipher

KEY_DIR = "vault_keys_test"
KEY_ID = "test-key"

def verify_flow():
    # 0. Clean up
    if os.path.exists(KEY_DIR):
        shutil.rmtree(KEY_DIR)
        
    print("1. Generating Keys...")
    # Using dimension 1024 for production
    dim = 1024
    keygen = KeyGenerator(key_path=KEY_DIR, key_id=KEY_ID, dim_list=[dim])
    keygen.generate_keys()
    
    # Keys found directly in KEY_DIR in this version/usage?
    # Or maybe KeyGenerator uses key_path as the output dir directly if provided?
    # Adjusting to observed behavior.
    enc_path = os.path.join(KEY_DIR, "EncKey.json")
    sec_path = os.path.join(KEY_DIR, "SecKey.json")
    
    if not os.path.exists(enc_path):
        raise FileNotFoundError(f"Key not found: {enc_path}")

    # 2. Init Cipher
    print(f"Initializing Cipher with dim={dim} and key={enc_path}")
    cipher = Cipher(enc_key_path=enc_path, dim=dim)
    
    # 3. Simulate Server: "Encrypting Scores"
    # Create scores for dim=1024. Most can be 0.
    mock_scores = np.zeros(dim, dtype=np.float32)
    mock_scores[:4] = [0.9, 0.1, 0.8, 0.2]
    print(f"Original Mock Scores (first 4): {mock_scores[:4]}")
    
    # Encrypt explicitly. Cipher.encrypt expects a vector (numpy array)
    # It returns a list of bytes usually (if multiple input) or single bytes?
    # Let's try encrypting as "item" or "query"? 
    # Actually we just want to encrypt a generic vector.
    # 'encrypt' method usually takes (data, type). Type might be 'item', 'query'.
    # For simulation, we just want to produce a ciphertext.
    # Let's try type="item".
    try:
        # Encrypt results
        encrypted_result = cipher.encrypt(mock_scores, "item")
        print(f"Encrypted result type: {type(encrypted_result)}")
        
        # 4. Simulate Vault: "Decrypting Scores"
        print("Decrypting using standard decrypt (simulating score decryption)...")
        # Use standard decrypt, which should work for encrypted vectors.
        decrypted_vector = cipher.decrypt(encrypted_result, sec_key_path=sec_path)
        
        # decrypted_vector is likely a numpy array or list of arrays
        print(f"Decrypted Vector type: {type(decrypted_vector)}")
        if isinstance(decrypted_vector, list) and len(decrypted_vector) > 0:
             print(f"Item 0 type: {type(decrypted_vector[0])}")
             print(f"Decrypted Vector (full): {decrypted_vector}")
        
        # Check similarity
        final_result = decrypted_vector
        # Only unwrap if it looks like a list of lists/arrays
        if isinstance(final_result, list) and len(final_result) > 0 and (isinstance(final_result[0], list) or isinstance(final_result[0], np.ndarray)):
             final_result = final_result[0]

        print(f"Decrypted: {final_result[:4]}")
        
        dec_arr = np.array(final_result)
        diff = np.abs(mock_scores - dec_arr[:len(mock_scores)])
        print(f"Max Diff: {np.max(diff)}")
        
        if np.max(diff) < 1e-4:
            print("SUCCESS: Crypto compatibility verified (via standard decrypt).")
        else:
            print("FAILURE: Decrypted values do not match.")
        
    except Exception as e:
        print(f"ERROR: {e}")
        import traceback
        traceback.print_exc()

if __name__ == "__main__":
    verify_flow()
