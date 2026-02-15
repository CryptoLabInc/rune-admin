from fastmcp import FastMCP
from mcp.types import ToolAnnotations
import base64
import pickle
import logging
import numpy as np
import os
import json
import time

logger = logging.getLogger("rune.vault")
from collections import defaultdict
from threading import Lock
from pyenvector.crypto import KeyGenerator, Cipher
from pyenvector.crypto.block import CipherBlock, Query
from pyenvector.utils.aes import decrypt_metadata as aes_decrypt_metadata
try:
    from pyenvector.proto_gen.v2.common.type_pb2 import CiphertextScore
except ModuleNotFoundError:
    from pyenvector.proto_gen.type_pb2 import CiphertextScore
import asyncio

try:
    import monitoring
    MONITORING_AVAILABLE = True
except ImportError:
    MONITORING_AVAILABLE = False
    # Dummy interface to prevent NameErrors if used without checking flag
    class DummyMonitoring:
        pass
    monitoring = DummyMonitoring()

# Configuration
KEY_DIR = "vault_keys"
KEY_ID = "vault-key"
DIM = 1024  # FHE cipher supports up to 2^12, using 1024 for production

# Initialize Keys on Startup
def ensure_keys():
    enc_key = os.path.join(KEY_DIR, "EncKey.json")
    if not os.path.exists(enc_key):
        logger.info(f"Generating keys in {KEY_DIR}...")
        os.makedirs(KEY_DIR, exist_ok=True)
        keygen = KeyGenerator(key_path=KEY_DIR, key_id=KEY_ID, dim_list=[DIM])
        keygen.generate_keys()
    else:
        logger.info(f"Keys found in {KEY_DIR}")

ensure_keys()
enc_key_path = os.path.join(KEY_DIR, "EncKey.json")
sec_key_path = os.path.join(KEY_DIR, "SecKey.json")
metadata_key_path = os.path.join(KEY_DIR, "MetadataKey.json")

# Initialize shared Cipher instance (thread-safety? FastMCP uses asyncio/uvicorn workers)
# Usually safe for read-ops.
cipher = Cipher(enc_key_path=enc_key_path, dim=DIM)

# =============================================================================
# Authorization
# =============================================================================
# DEMO TOKENS - DO NOT USE IN PRODUCTION
# Replace with your own tokens after signing up at https://envector.io
#
# Production setup:
#   export VAULT_TOKENS="your-token-1,your-token-2"
# =============================================================================
_ENV_TOKENS = os.getenv("VAULT_TOKENS", "").strip()
if _ENV_TOKENS:
    VALID_TOKENS = set(filter(None, _ENV_TOKENS.split(",")))
else:
    # Demo tokens for local testing only
    VALID_TOKENS = {
        "TOKEN-FOR-DEMONSTRATION-PURPOSES-ONLY-DO-NOT-USE-IN-PRODUCTION",
    }
    logger.warning("Using demo tokens. Set VAULT_TOKENS env var for production.")

# Team index name (set by admin, distributed to all team members via get_public_key)
VAULT_INDEX_NAME = os.getenv("VAULT_INDEX_NAME", "").strip() or None
if VAULT_INDEX_NAME:
    logger.info(f"Team index name configured: {VAULT_INDEX_NAME}")
else:
    logger.info("No team index name configured (VAULT_INDEX_NAME not set)")


# =============================================================================
# Rate Limiting
# =============================================================================
class RateLimiter:
    """Simple sliding window rate limiter."""

    def __init__(self, max_requests: int = 30, window_seconds: int = 60):
        self.max_requests = max_requests
        self.window_seconds = window_seconds
        self._requests: dict[str, list[float]] = defaultdict(list)
        self._lock = Lock()

    def is_allowed(self, client_id: str) -> bool:
        """Check if request is allowed and record it."""
        now = time.time()
        with self._lock:
            # Clean old entries
            self._requests[client_id] = [
                t for t in self._requests[client_id]
                if now - t < self.window_seconds
            ]
            # Check limit
            if len(self._requests[client_id]) >= self.max_requests:
                return False
            # Record request
            self._requests[client_id].append(now)
            return True

    def get_retry_after(self, client_id: str) -> int:
        """Returns seconds until next request is allowed."""
        with self._lock:
            if not self._requests[client_id]:
                return 0
            oldest = min(self._requests[client_id])
            return max(0, int(self.window_seconds - (time.time() - oldest)))


rate_limiter = RateLimiter(max_requests=30, window_seconds=60)


def validate_token(token: str):
    """Validate authentication token with rate limiting."""
    # Rate limit by token (prevents brute-force)
    if not rate_limiter.is_allowed(token):
        retry_after = rate_limiter.get_retry_after(token)
        raise ValueError(f"Rate limit exceeded. Retry after {retry_after} seconds.")

    if token not in VALID_TOKENS:
        raise ValueError("Access Denied: Invalid authentication token")

# Core Business Logic (testable without MCP framework)
def _get_public_key_impl(token: str) -> str:
    """
    Core implementation: Returns the public key bundle.
    
    Args:
        token: Authentication token issued by Vault Admin.
        
    Returns:
        JSON string containing EncKey, EvalKey.
    """
    validate_token(token)
    
    bundle = {}
    for filename in ["EncKey.json", "EvalKey.json"]:
        path = os.path.join(KEY_DIR, filename)
        if os.path.exists(path):
            with open(path, "r") as f:
                bundle[filename] = f.read()
        else:
            # Should not happen if ensure_keys ran
            pass

    # Include team index name if configured by admin
    if VAULT_INDEX_NAME:
        bundle["index_name"] = VAULT_INDEX_NAME

    return json.dumps(bundle)

# MCP Server
mcp = FastMCP("enVector-Vault")

@mcp.tool(annotations=ToolAnnotations(readOnlyHint=True, destructiveHint=False))
def get_public_key(token: str) -> str:
    """
    Returns the public key bundle (EncKey, EvalKey).
    This bundle allows the Agent to encrypt data/queries and register keys with the Cloud.

    Args:
        token: Authentication token issued by Vault Admin.

    Returns:
        JSON string containing:
        {
            "EncKey.json": "...",
            "EvalKey.json": "..."
        }
    """
    start_time = time.time()
    status = "success"
    try:
        result = _get_public_key_impl(token)
        # Check for soft errors in JSON response
        try:
             data = json.loads(result)
             if "error" in data:
                 status = "error"
        except Exception:
             pass
        return result
    except Exception:
        status = "error"
        raise
    finally:
        if MONITORING_AVAILABLE:
            duration = time.time() - start_time
            monitoring.vault_requests_total.labels(method="get_public_key", endpoint="tool", status=status).inc()
            monitoring.vault_request_duration.labels(method="get_public_key", endpoint="tool").observe(duration)

def _decrypt_scores_impl(token: str, encrypted_blob_b64: str, top_k: int = 5) -> str:
    """
    Core implementation: Decrypts CiphertextScore and applies Top-K filtering.

    The blob is a protobuf-serialized CiphertextScore produced by Index.scoring().
    cipher.decrypt_score() returns {"score": [[s0, s1, ...], ...], "shard_idx": [...]},
    where each inner list corresponds to a shard (IVF) or a single chunk (FLAT).

    Args:
        token: Authentication token issued by Vault Admin.
        encrypted_blob_b64: Base64 string of the serialized CiphertextScore protobuf.
        top_k: Number of top results to return (max 10 allowed).

    Returns:
        JSON string containing the list of {shard_idx, row_idx, score}.
    """
    validate_token(token)

    # Policy Enforcement
    if top_k > 10:
        return json.dumps({"error": "Rate Limit Exceeded: Max top_k is 10"})

    try:
        # 1. Deserialize CiphertextScore protobuf
        blob_bytes = base64.b64decode(encrypted_blob_b64)

        try:
            score_proto = CiphertextScore()
            score_proto.ParseFromString(blob_bytes)
            encrypted_result = CipherBlock(data=score_proto)
        except Exception as e:
            return json.dumps({"error": f"Deserialization failed: {str(e)}"})

        # 2. Decrypt with cipher.decrypt_score (NOT cipher.decrypt)
        decrypted = cipher.decrypt_score(encrypted_result, sec_key_path=sec_key_path)
        # decrypted: {"score": [[chunk0_scores], [chunk1_scores], ...], "shard_idx": [s0, s1, ...]}
        score_2d = decrypted["score"]
        shard_indices = decrypted.get("shard_idx", list(range(len(score_2d))))

        # 3. Top-K across all shards (handles both FLAT and IVF_FLAT)
        # Flatten 2D scores into (shard_idx, row_idx, score) tuples
        import heapq
        all_scores = (
            (shard_indices[i], j, float(v))
            for i, row in enumerate(score_2d)
            for j, v in enumerate(row)
        )
        topk_results = heapq.nlargest(top_k, all_scores, key=lambda x: x[2])

        params = [
            {"shard_idx": s, "row_idx": r, "score": sc}
            for s, r, sc in topk_results
        ]

        return json.dumps(params)

    except Exception as e:
        return json.dumps({"error": str(e)})

@mcp.tool(annotations=ToolAnnotations(readOnlyHint=True, destructiveHint=False))
def decrypt_scores(token: str, encrypted_blob_b64: str, top_k: int = 5) -> str:
    """
    Decrypts a blob of encrypted scores using the Vault's Secret Key.
    Applies Top-K filtering and returns the result.

    Args:
        token: Authentication token issued by Vault Admin.
        encrypted_blob_b64: Base64 string of the serialized CipherBlock (Query) from the Cloud.
        top_k: Number of top results to return (max 10 allowed).

    Returns:
        JSON string containing the list of scores (and implicitly indices).
    """
    start_time = time.time()
    status = "success"
    try:
        result = _decrypt_scores_impl(token, encrypted_blob_b64, top_k)
        # Check for soft errors
        try:
             data = json.loads(result)
             if "error" in data:
                 status = "error"
        except Exception:
             pass
        return result
    except Exception:
        status = "error"
        raise
    finally:
        if MONITORING_AVAILABLE:
            duration = time.time() - start_time
            monitoring.vault_requests_total.labels(method="decrypt_scores", endpoint="tool", status=status).inc()
            monitoring.vault_request_duration.labels(method="decrypt_scores", endpoint="tool").observe(duration)

def _decrypt_metadata_impl(token: str, encrypted_metadata_list: list[str]) -> str:
    """
    Core implementation: Decrypts a list of AES-encrypted metadata strings
    using the Vault's MetadataKey.

    Args:
        token: Authentication token issued by Vault Admin.
        encrypted_metadata_list: List of Base64-encoded encrypted metadata strings.

    Returns:
        JSON string containing the list of decrypted metadata objects.
    """
    validate_token(token)

    if not os.path.exists(metadata_key_path):
        return json.dumps({"error": "MetadataKey not found in Vault"})

    try:
        results = []
        for token_b64 in encrypted_metadata_list:
            decrypted = aes_decrypt_metadata(token_b64, metadata_key_path)
            results.append(decrypted)
        return json.dumps(results)
    except Exception as e:
        return json.dumps({"error": f"Metadata decryption failed: {str(e)}"})


@mcp.tool(annotations=ToolAnnotations(readOnlyHint=True, destructiveHint=False))
def decrypt_metadata(token: str, encrypted_metadata_list: list[str]) -> str:
    """
    Decrypts a list of AES-encrypted metadata using the Vault's MetadataKey.
    The MetadataKey never leaves Vault — Agent sends encrypted blobs, receives plaintext.

    Args:
        token: Authentication token issued by Vault Admin.
        encrypted_metadata_list: List of Base64-encoded encrypted metadata strings from enVector Cloud.

    Returns:
        JSON string containing the list of decrypted metadata (original format preserved).
    """
    start_time = time.time()
    status = "success"
    try:
        result = _decrypt_metadata_impl(token, encrypted_metadata_list)
        try:
            data = json.loads(result)
            if isinstance(data, dict) and "error" in data:
                status = "error"
        except Exception:
            pass
        return result
    except Exception:
        status = "error"
        raise
    finally:
        if MONITORING_AVAILABLE:
            duration = time.time() - start_time
            monitoring.vault_requests_total.labels(method="decrypt_metadata", endpoint="tool", status=status).inc()
            monitoring.vault_request_duration.labels(method="decrypt_metadata", endpoint="tool").observe(duration)


if __name__ == "__main__":
    import sys
    import argparse
    
    parser = argparse.ArgumentParser(description="Run the enVector-Vault MCP server.")
    parser.add_argument("command", nargs="?", choices=["server"], help="Command to run")
    parser.add_argument("--mode", choices=["sse", "http"], default="sse", help="Transport mode")
    parser.add_argument("--port", type=int, default=50080, help="Port to bind")
    parser.add_argument("--host", default="0.0.0.0", help="Host to bind")
    
    args = parser.parse_args()

    if args.command == "server":
        logger.info(f"Starting enVector-Vault MCP Server on {args.host}:{args.port}...")

        import uvicorn
        # FastMCP 2.x uses http_app(), fallback to sse_app() for older versions
        if hasattr(mcp, 'http_app'):
            app = mcp.http_app()
        else:
            app = mcp.sse_app()

        if MONITORING_AVAILABLE:
            # Add monitoring endpoints (health, metrics)
            monitoring.add_monitoring_endpoints(app)

            # Start health check background task
            @app.on_event("startup")
            async def startup_event():
                asyncio.create_task(monitoring.periodic_health_check())
        else:
            logger.warning("Monitoring module not available. Skipping /health and /metrics.")

        uvicorn.run(app, host=args.host, port=args.port)
            
    else:
        # Default to stdio for CLI / Inspector usage
        mcp.run()
