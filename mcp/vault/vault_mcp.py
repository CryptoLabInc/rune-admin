from fastmcp import FastMCP
import base64
import pickle
import numpy as np
import os
import json
import time
from collections import defaultdict
from threading import Lock
from pyenvector.crypto import KeyGenerator, Cipher
from pyenvector.crypto.block import CipherBlock, Query
from pyenvector.proto_gen.v2.common.type_pb2 import CiphertextScore
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
    if not os.path.exists(KEY_DIR):
        print(f"Generating keys in {KEY_DIR}...")
        keygen = KeyGenerator(key_path=KEY_DIR, key_id=KEY_ID, dim_list=[DIM])
        keygen.generate_keys()
    else:
        print(f"Keys found in {KEY_DIR}")

ensure_keys()
enc_key_path = os.path.join(KEY_DIR, "EncKey.json")
sec_key_path = os.path.join(KEY_DIR, "SecKey.json")

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
        "DEMO-TOKEN-GET-YOUR-OWN-AT-ENVECTOR-IO",
        "DEMO-ADMIN-SIGNUP-AT-ENVECTOR-IO"
    }
    print("WARNING: Using demo tokens. Set VAULT_TOKENS env var for production.")


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
        JSON string containing EncKey, EvalKey, MetadataKey.
    """
    validate_token(token)
    
    bundle = {}
    for filename in ["EncKey.json", "EvalKey.json", "MetadataKey.json"]:
        path = os.path.join(KEY_DIR, filename)
        if os.path.exists(path):
            with open(path, "r") as f:
                bundle[filename] = f.read()
        else:
            # Should not happen if ensure_keys ran
            pass
            
    return json.dumps(bundle)

# MCP Server
mcp = FastMCP("enVector-Vault")

@mcp.tool()
def get_public_key(token: str) -> str:
    """
    Returns the public key bundle (EncKey, EvalKey, MetadataKey).
    This bundle allows the Agent to encrypt data/queries and register keys with the Cloud.
    
    Args:
        token: Authentication token issued by Vault Admin.
        
    Returns:
        JSON string containing:
        {
            "EncKey.json": "...",
            "EvalKey.json": "...",
            "MetadataKey.json": "..."
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

@mcp.tool()
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
        print(f"Starting enVector-Vault MCP Server (SSE) on {args.host}:{args.port}...")
        
        # SSE is standard for FastMCP
        import uvicorn
        # Note: sse_app() returns a Starlette/FastAPI app
        app = mcp.sse_app()
        
        if MONITORING_AVAILABLE:
            # Add monitoring endpoints (health, metrics)
            monitoring.add_monitoring_endpoints(app)
            
            # Start health check background task
            @app.on_event("startup")
            async def startup_event():
                asyncio.create_task(monitoring.periodic_health_check())
        else:
            print("WARNING: Monitoring module not available. skipping /health and /metrics.")
            
        uvicorn.run(app, host=args.host, port=args.port)
            
    else:
        # Default to stdio for CLI / Inspector usage
        mcp.run()
