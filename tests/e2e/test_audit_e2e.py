"""
E2E test for structured audit logging.

Starts a real gRPC server, sends requests through all three endpoints,
and verifies that the audit log file contains correct JSON entries.

Prerequisites:
  - Proto stubs generated (bash vault/scripts/proto-gen.sh)
  - All vault dependencies installed (pip install -r vault/requirements.txt)

Usage:
  REPO_ROOT=$(pwd) python tests/e2e/test_audit_e2e.py
"""

import json
import os
import sys
import tempfile
import time

# Configure audit BEFORE importing vault modules
audit_log_path = tempfile.mktemp(suffix=".log", prefix="audit-e2e-")
os.environ["VAULT_AUDIT_LOG"] = f"file:{audit_log_path}+stdout"
os.environ["VAULT_TLS_DISABLE"] = "true"

REPO_ROOT = os.environ.get("REPO_ROOT", os.getcwd())
vault_dir = os.path.join(REPO_ROOT, "vault")
sys.path.insert(0, vault_dir)
sys.path.insert(0, os.path.join(vault_dir, "proto"))

import grpc
from proto import vault_service_pb2 as pb2
from proto import vault_service_pb2_grpc as pb2_grpc

# Must match token_store.DEMO_TOKEN (36 chars for protovalidate)
DEMO_TOKEN = "evt_0000000000000000000000000000demo"
BAD_TOKEN = "evt_0000000000000000000000000invalid"

GRPC_PORT = 50099


def main():
    from vault_grpc_server import serve_grpc

    server = serve_grpc(host="127.0.0.1", port=GRPC_PORT)
    time.sleep(0.5)

    try:
        channel = grpc.insecure_channel(f"127.0.0.1:{GRPC_PORT}", options=[
            ("grpc.max_receive_message_length", 256 * 1024 * 1024),
        ])
        stub = pb2_grpc.VaultServiceStub(channel)

        # 1) Success path
        print("=" * 60)
        print("1) GetPublicKey — valid demo token (expect success)")
        print("=" * 60)
        resp = stub.GetPublicKey(pb2.GetPublicKeyRequest(token=DEMO_TOKEN))
        assert resp.key_bundle_json, "Expected key bundle"
        print(f"   -> success, has_key=True")

        # 2) Denied path — invalid token
        print()
        print("=" * 60)
        print("2) GetPublicKey — invalid token (expect denied)")
        print("=" * 60)
        try:
            stub.GetPublicKey(pb2.GetPublicKeyRequest(token=BAD_TOKEN))
            print("   -> unexpected success")
        except grpc.RpcError as e:
            assert e.code() == grpc.StatusCode.UNAUTHENTICATED
            print(f"   -> {e.code().name}: {e.details()}")

        # 3) Error path — valid token, bad payload
        print()
        print("=" * 60)
        print("3) DecryptScores — valid token, bad blob (expect error)")
        print("=" * 60)
        resp = stub.DecryptScores(pb2.DecryptScoresRequest(
            token=DEMO_TOKEN,
            encrypted_blob_b64="not-a-valid-blob",
            top_k=5,
        ))
        assert resp.error, "Expected error response"
        print(f"   -> error: {resp.error}")

        # 4) Denied path — decrypt_metadata with invalid token
        print()
        print("=" * 60)
        print("4) DecryptMetadata — invalid token (expect denied)")
        print("=" * 60)
        try:
            stub.DecryptMetadata(pb2.DecryptMetadataRequest(
                token=BAD_TOKEN,
                encrypted_metadata_list=["test"],
            ))
            print("   -> unexpected success")
        except grpc.RpcError as e:
            assert e.code() == grpc.StatusCode.UNAUTHENTICATED
            print(f"   -> {e.code().name}: {e.details()}")

        channel.close()
    finally:
        server.stop(grace=1)

    # -- Verify audit log file --
    time.sleep(0.3)
    print()
    print("=" * 60)
    print(f"AUDIT LOG: {audit_log_path}")
    print("=" * 60)

    with open(audit_log_path) as f:
        entries = [json.loads(line.strip()) for line in f if line.strip()]

    required_fields = {
        "timestamp", "user_id", "method", "top_k",
        "result_count", "status", "source_ip", "latency_ms",
    }

    for i, entry in enumerate(entries, 1):
        print(f"\n--- Entry {i} ---")
        print(json.dumps(entry, indent=2))
        missing = required_fields - entry.keys()
        assert not missing, f"Entry {i} missing fields: {missing}"

    # Verify expected statuses
    statuses = [e["status"] for e in entries]
    assert statuses == ["success", "denied", "error", "denied"], \
        f"Unexpected status sequence: {statuses}"

    os.unlink(audit_log_path)
    print(f"\nAll {len(entries)} audit entries verified.")


if __name__ == "__main__":
    main()
