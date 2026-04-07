#!/usr/bin/env python3
"""
Generate test fixtures for integration tests.

Connects to enVector Cloud to capture a real CiphertextScore blob,
then generates metadata envelopes locally. All fixtures are saved
to tests/fixtures/ for use in CI without cloud access.

Usage:
    ENVECTOR_ENDPOINT=... ENVECTOR_API_KEY=... python scripts/generate-test-fixtures.py
"""

import base64
import hashlib
import json
import os
import shutil
import sys
import tempfile

import numpy as np

FIXTURES_DIR = os.path.join(os.path.dirname(__file__), "..", "tests", "fixtures")
TEAM_SECRET = "fixture-team-secret-for-testing"
TOKEN = "evt_0000000000000000000000000000demo"
AGENT_ID = hashlib.sha256(TOKEN.encode("utf-8")).hexdigest()[:32]
DIM = 768
KEY_ID = "test-fixture"
INDEX_NAME = "test_fixture_index"


def main():
    endpoint = os.getenv("ENVECTOR_ENDPOINT")
    api_key = os.getenv("ENVECTOR_API_KEY")
    if not endpoint or not api_key:
        print("Error: ENVECTOR_ENDPOINT and ENVECTOR_API_KEY must be set.")
        sys.exit(1)

    import pyenvector as ev
    from cryptography.hazmat.primitives import hashes
    from cryptography.hazmat.primitives.kdf.hkdf import HKDF
    from pyenvector.crypto import Cipher, KeyGenerator
    from pyenvector.utils.aes import encrypt_metadata

    try:
        from pyenvector.proto_gen.v2.common.type_pb2 import CiphertextScore
    except ModuleNotFoundError:
        from pyenvector.proto_gen.type_pb2 import CiphertextScore

    # ── Step 1: Generate FHE keys in temp dir ────────────────────────
    print("==> Generating FHE keys...")
    tmp_key_dir = tempfile.mkdtemp(prefix="fixture_keys_")
    key_subdir = os.path.join(tmp_key_dir, KEY_ID)
    keygen = KeyGenerator(key_path=tmp_key_dir, key_id=KEY_ID, dim_list=[DIM])
    keygen.generate_keys()
    print(f"    Keys generated in {key_subdir}")

    sec_key_path = os.path.join(key_subdir, "SecKey.json")
    enc_key_path = os.path.join(key_subdir, "EncKey.json")

    # ── Step 2: Connect to enVector Cloud and reset server-side keys ─
    print(f"==> Connecting to enVector Cloud ({endpoint})...")
    # First connect without auto_key_setup to clean up stale server state
    ev.init(
        address=endpoint,
        key_path=tmp_key_dir,
        key_id=KEY_ID,
        dim=DIM,
        eval_mode="rmp",
        auto_key_setup=False,
        access_token=api_key,
        query_encryption="plain",
    )
    try:
        ev.delete_key(KEY_ID)
        print(f"    Deleted stale server key '{KEY_ID}'.")
    except Exception:
        pass
    try:
        ev.drop_index(INDEX_NAME)
        print(f"    Deleted stale index '{INDEX_NAME}'.")
    except Exception:
        pass

    # Reconnect with auto_key_setup to register the fresh keys
    ev.init(
        address=endpoint,
        key_path=tmp_key_dir,
        key_id=KEY_ID,
        dim=DIM,
        eval_mode="rmp",
        auto_key_setup=True,
        access_token=api_key,
        query_encryption="plain",
    )
    print("    Connected with fresh keys.")

    # ── Step 3: Create index and insert vectors ──────────────────────
    print(f"==> Creating index '{INDEX_NAME}'...")
    try:
        ev.create_index(
            index_name=INDEX_NAME,
            dim=DIM,
            index_params={"index_type": "FLAT"},
            query_encryption="plain",
            metadata_encryption=False,
            metadata_key=b"",
        )
        print("    Index created.")
    except Exception as e:
        print(f"    Index may already exist: {e}")

    print("==> Generating test vectors with controlled similarities...")
    np.random.seed(42)

    # Generate query vector first
    query = np.random.randn(DIM)
    query = query / np.linalg.norm(query)

    # Create document vectors with target similarities spread across 0.3 ~ 0.9
    target_sims = [0.9, 0.8, 0.7, 0.6, 0.5, 0.45, 0.4, 0.35, 0.3, 0.55]
    vectors = []
    for alpha in target_sims:
        noise = np.random.randn(DIM)
        noise = noise / np.linalg.norm(noise)
        vec = alpha * query + np.sqrt(1 - alpha**2) * noise  # blend with query
        vec = vec / np.linalg.norm(vec)  # re-normalize
        vectors.append(vec)
    vectors = np.array(vectors)
    vectors = vectors.tolist()
    metadata = [f"doc_{i}" for i in range(10)]

    from pyenvector.index import Index
    index = Index(index_name=INDEX_NAME)
    index.insert(data=vectors, metadata=metadata)
    print(f"    Inserted {len(vectors)} vectors.")

    # ── Step 4: Run scoring to capture CiphertextScore ───────────────
    print("==> Running scoring query...")
    query = query.tolist()
    results = index.scoring(query=query)
    score_block = results[0]
    score_proto_bytes = score_block.data.SerializeToString()
    score_b64 = base64.b64encode(score_proto_bytes).decode("utf-8")
    print(f"    CiphertextScore captured ({len(score_proto_bytes)} bytes).")

    # ── Step 5: Decrypt locally for expected output ──────────────────
    print("==> Decrypting scores locally for expected output...")
    cipher = Cipher(enc_key_path=enc_key_path, dim=DIM)
    raw_decrypted = cipher.decrypt_score(score_block, sec_key_path=sec_key_path)
    # Convert protobuf containers to plain Python lists for JSON serialization
    decrypted = {
        "score": [list(row) for row in raw_decrypted["score"]],
        "shard_idx": list(raw_decrypted.get("shard_idx", [])),
    }
    print(f"    Decrypted: {len(decrypted['score'])} shards.")

    # ── Step 6: Generate metadata envelopes ──────────────────────────
    print("==> Generating metadata envelopes...")
    hkdf = HKDF(
        algorithm=hashes.SHA256(),
        length=32,
        salt=None,
        info=AGENT_ID.encode("utf-8"),
    )
    agent_dek = hkdf.derive(TEAM_SECRET.encode("utf-8"))

    plaintexts = [
        {"title": "Test Document 1", "content": "Hello world"},
        {"title": "Test Document 2", "content": "Integration test data"},
        {"title": "Test Document 3", "content": "Fixture metadata"},
    ]

    envelopes = []
    for pt in plaintexts:
        ct_b64 = encrypt_metadata(pt, agent_dek)
        envelopes.append(json.dumps({"a": AGENT_ID, "c": ct_b64}))
    print(f"    Generated {len(envelopes)} envelopes.")

    # ── Step 7: Cleanup index ────────────────────────────────────────
    print(f"==> Cleaning up index '{INDEX_NAME}'...")
    try:
        index.drop()
        print("    Index deleted.")
    except Exception as e:
        print(f"    Cleanup (manual deletion may be needed): {e}")

    # ── Step 8: Save fixtures ────────────────────────────────────────
    print(f"==> Saving fixtures to {FIXTURES_DIR}...")
    os.makedirs(FIXTURES_DIR, exist_ok=True)

    # FHE Keys (filenames must match pyenvector conventions: EncKey.json, SecKey.json)
    keys_dir = os.path.join(FIXTURES_DIR, "keys")
    os.makedirs(keys_dir, exist_ok=True)
    shutil.copy2(sec_key_path, os.path.join(keys_dir, "SecKey.json"))
    shutil.copy2(enc_key_path, os.path.join(keys_dir, "EncKey.json"))

    # CiphertextScore blob
    with open(os.path.join(FIXTURES_DIR, "ciphertext_score.b64"), "w") as f:
        f.write(score_b64)

    # Expected scores
    with open(os.path.join(FIXTURES_DIR, "expected_scores.json"), "w") as f:
        json.dump(decrypted, f, indent=2)

    # Metadata envelopes
    with open(os.path.join(FIXTURES_DIR, "metadata_envelopes.json"), "w") as f:
        json.dump(envelopes, f, indent=2)

    # Expected metadata plaintext
    with open(os.path.join(FIXTURES_DIR, "expected_metadata.json"), "w") as f:
        json.dump(plaintexts, f, indent=2)

    # Config
    config = {
        "team_secret": TEAM_SECRET,
        "agent_id": AGENT_ID,
        "token": TOKEN,
        "key_id": KEY_ID,
        "dim": DIM,
    }
    with open(os.path.join(FIXTURES_DIR, "config.json"), "w") as f:
        json.dump(config, f, indent=2)

    # Cleanup temp keys
    shutil.rmtree(tmp_key_dir, ignore_errors=True)

    print("==> Done. Fixtures saved to tests/fixtures/")


if __name__ == "__main__":
    main()
