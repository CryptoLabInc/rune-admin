from fastmcp import FastMCP
import base64
import pickle
import numpy as np
import os
import json
import time
import uuid
import logging
from datetime import datetime
from collections import defaultdict
from threading import Lock
from typing import Optional, Dict, Any, List
from pyenvector.crypto import KeyGenerator, Cipher
from pyenvector.crypto.block import CipherBlock, Query

# =============================================================================
# Audit Logging
# =============================================================================
AUDIT_LOG_PATH = os.getenv("AUDIT_LOG_PATH", "vault_audit.log")
ENABLE_AUDIT_LOG = os.getenv("ENABLE_AUDIT_LOG", "true").lower() == "true"

# Configure audit logger
audit_logger = logging.getLogger("vault_audit")
audit_logger.setLevel(logging.INFO)
if ENABLE_AUDIT_LOG:
    handler = logging.FileHandler(AUDIT_LOG_PATH)
    handler.setFormatter(logging.Formatter(
        '%(asctime)s | %(levelname)s | %(message)s',
        datefmt='%Y-%m-%d %H:%M:%S'
    ))
    audit_logger.addHandler(handler)


def audit_log(
    operation: str,
    token: str,
    request_id: Optional[str] = None,
    status: str = "success",
    details: Optional[Dict[str, Any]] = None,
    latency_ms: Optional[float] = None
):
    """Write security audit log entry."""
    if not ENABLE_AUDIT_LOG:
        return

    # Mask token for logging (show first 8 chars only)
    masked_token = token[:8] + "..." if len(token) > 8 else token

    log_entry = {
        "operation": operation,
        "token": masked_token,
        "request_id": request_id or "N/A",
        "status": status,
        "latency_ms": latency_ms,
        "details": details or {}
    }

    if status == "success":
        audit_logger.info(json.dumps(log_entry))
    else:
        audit_logger.warning(json.dumps(log_entry))

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
    return _get_public_key_impl(token)

def _decrypt_scores_impl(token: str, encrypted_blob_b64: str, top_k: int = 5) -> str:
    """
    Core implementation: Decrypts scores and applies Top-K filtering.
    
    Args:
        token: Authentication token issued by Vault Admin.
        encrypted_blob_b64: Base64 string of the serialized CipherBlock.
        top_k: Number of top results to return (max 10 allowed).
    
    Returns:
        JSON string containing the list of scores.
    """
    validate_token(token)
    
    # Policy Enforcement
    if top_k > 10:
        return json.dumps({"error": "Rate Limit Exceeded: Max top_k is 10"})

    try:
        # 1. Deserialize
        blob_bytes = base64.b64decode(encrypted_blob_b64)
        
        # Native deserialization
        # `CipherBlock.serialize` returns the raw bytes compatible with Query/Score serialization.
        # Since we encrypted a vector ("item" or "query"), it's likely a Query object internally (or CiphertextScore if score).
        # We assume it's a Query object for now as observed in verify_crypto.
        
        try:
             query_obj = Query.deserializeFrom(blob_bytes)
             encrypted_result = CipherBlock(data=query_obj)
        except Exception as e:
             # Fallback: maybe it was pickling? No, we moved to native.
             return json.dumps({"error": f"Deserialization failed: {str(e)}"})
        
        # 2. Decrypt
        decrypted_vector = cipher.decrypt(encrypted_result, sec_key_path=sec_key_path)
        
        # unwrapping list logic (from verification script)
        if isinstance(decrypted_vector, list) and len(decrypted_vector) > 0 and (isinstance(decrypted_vector[0], list) or isinstance(decrypted_vector[0], np.ndarray)):
            decrypted_vector = decrypted_vector[0]
            
        # 3. Top-K
        scores = np.array(decrypted_vector)
        # Get indices of top_k scores (descending)
        # Note: argpartition is faster but argsort is easier for full sort
        if len(scores) <= top_k:
            top_indices = np.argsort(scores)[::-1]
        else:
            top_indices = np.argpartition(scores, -top_k)[-top_k:]
            top_indices = top_indices[np.argsort(scores[top_indices])[::-1]]
            
        params = []
        for idx in top_indices:
            params.append({
                "index": int(idx),
                "score": float(scores[idx])
            })
            
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
    return _decrypt_scores_impl(token, encrypted_blob_b64, top_k)


# =============================================================================
# Enhanced Decrypt API with Audit Trail (for Agent Integration)
# =============================================================================

def _decrypt_search_results_impl(
    token: str,
    encrypted_blob_b64: str,
    top_k: int = 5,
    request_id: Optional[str] = None,
    include_all_scores: bool = False
) -> Dict[str, Any]:
    """
    Core implementation: Decrypts search results from enVector Cloud.

    This is the primary API for Rune agents. It provides:
    - Structured response format
    - Request ID tracking for audit trail
    - Policy enforcement (max top_k)

    Args:
        token: Authentication token issued by Vault Admin.
        encrypted_blob_b64: Base64 string of the serialized CipherBlock from enVector Cloud.
        top_k: Number of top results to return (max 10 allowed).
        request_id: Optional correlation ID for audit trail.
        include_all_scores: If True, include all decrypted scores (for debugging only).

    Returns:
        Dict with keys:
        - ok: bool (success status)
        - results: List[{index: int, score: float}] (top-k results)
        - request_id: str (correlation ID)
        - timestamp: float (Unix timestamp)
        - error: str (only if ok=False)
    """
    start_time = time.time()
    request_id = request_id or f"vault_{uuid.uuid4().hex[:12]}"

    try:
        # 1. Validate token (includes rate limiting)
        validate_token(token)

        # 2. Policy enforcement
        if top_k > 10:
            raise ValueError("Policy Violation: Max top_k is 10")
        if top_k < 1:
            raise ValueError("Policy Violation: top_k must be at least 1")

        # 3. Deserialize encrypted blob
        try:
            blob_bytes = base64.b64decode(encrypted_blob_b64)
            query_obj = Query.deserializeFrom(blob_bytes)
            encrypted_result = CipherBlock(data=query_obj)
        except Exception as e:
            raise ValueError(f"Deserialization failed: {str(e)}")

        # 4. Decrypt using SecKey
        decrypted_vector = cipher.decrypt(encrypted_result, sec_key_path=sec_key_path)

        # Unwrap nested list if needed
        if isinstance(decrypted_vector, list) and len(decrypted_vector) > 0:
            if isinstance(decrypted_vector[0], (list, np.ndarray)):
                decrypted_vector = decrypted_vector[0]

        # 5. Apply Top-K selection
        scores = np.array(decrypted_vector)
        total_vectors = len(scores)

        if total_vectors <= top_k:
            top_indices = np.argsort(scores)[::-1]
        else:
            top_indices = np.argpartition(scores, -top_k)[-top_k:]
            top_indices = top_indices[np.argsort(scores[top_indices])[::-1]]

        # 6. Build structured results
        results = []
        for idx in top_indices:
            results.append({
                "index": int(idx),
                "score": float(scores[idx])
            })

        latency_ms = (time.time() - start_time) * 1000

        # 7. Audit log
        audit_log(
            operation="decrypt_search_results",
            token=token,
            request_id=request_id,
            status="success",
            details={
                "top_k": top_k,
                "total_vectors": total_vectors,
                "results_returned": len(results),
                "max_score": float(results[0]["score"]) if results else 0
            },
            latency_ms=latency_ms
        )

        response = {
            "ok": True,
            "results": results,
            "request_id": request_id,
            "timestamp": time.time(),
            "total_vectors": total_vectors
        }

        # Include all scores only for debugging (disabled by default)
        if include_all_scores:
            response["all_scores"] = [float(s) for s in scores]

        return response

    except ValueError as e:
        latency_ms = (time.time() - start_time) * 1000
        audit_log(
            operation="decrypt_search_results",
            token=token,
            request_id=request_id,
            status="error",
            details={"error": str(e)},
            latency_ms=latency_ms
        )
        return {
            "ok": False,
            "error": str(e),
            "request_id": request_id,
            "timestamp": time.time()
        }

    except Exception as e:
        latency_ms = (time.time() - start_time) * 1000
        audit_log(
            operation="decrypt_search_results",
            token=token,
            request_id=request_id,
            status="error",
            details={"error": str(e), "type": type(e).__name__},
            latency_ms=latency_ms
        )
        return {
            "ok": False,
            "error": f"Internal error: {str(e)}",
            "request_id": request_id,
            "timestamp": time.time()
        }


@mcp.tool()
def decrypt_search_results(
    token: str,
    encrypted_blob_b64: str,
    top_k: int = 5,
    request_id: str = ""
) -> str:
    """
    Decrypts encrypted search results from enVector Cloud.

    This is the primary decryption API for Rune agents. The MCP server sends
    encrypted results here, and Vault decrypts them using the SecKey that
    only Vault possesses.

    Security Model:
    - SecKey never leaves Vault
    - All decryption requests are logged with audit trail
    - Max 10 results per request (policy enforced)
    - Rate limiting per token

    Args:
        token: Authentication token issued by Vault Admin.
        encrypted_blob_b64: Base64-encoded encrypted result blob from enVector Cloud.
        top_k: Number of top results to return (max 10, default 5).
        request_id: Optional correlation ID for audit trail (auto-generated if empty).

    Returns:
        JSON string containing:
        {
            "ok": true/false,
            "results": [{"index": 0, "score": 0.95}, ...],
            "request_id": "vault_abc123",
            "timestamp": 1234567890.123,
            "total_vectors": 1000,
            "error": "..." (only if ok=false)
        }
    """
    result = _decrypt_search_results_impl(
        token=token,
        encrypted_blob_b64=encrypted_blob_b64,
        top_k=top_k,
        request_id=request_id if request_id else None
    )
    return json.dumps(result)


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

        # Integrated Monitoring
        from monitoring import add_monitoring_endpoints, periodic_health_check
        import asyncio

        # Add endpoints (/health, /metrics)
        add_monitoring_endpoints(app)

        # Start background health checker
        @app.on_event("startup")
        async def startup_event():
            asyncio.create_task(periodic_health_check())
            print("Monitoring started")

        uvicorn.run(app, host=args.host, port=args.port)
            
    else:
        # Default to stdio for CLI / Inspector usage
        mcp.run()
