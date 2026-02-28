"""
Rune-Vault Core Business Logic

Pure business logic for FHE key management, authentication, and decryption.
No transport layer (MCP, gRPC) — consumed by vault_grpc_server.py.
"""

import base64
import functools
import hashlib
import heapq
import hmac
import logging
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

# ev.init() resolves key files as {KEY_DIR}/{KEY_ID}/EncKey.json
KEY_SUBDIR = os.path.join(KEY_DIR, KEY_ID)

# enVector Cloud configuration
ENVECTOR_ENDPOINT = os.getenv("ENVECTOR_ENDPOINT", "").strip() or None
ENVECTOR_API_KEY = os.getenv("ENVECTOR_API_KEY", "").strip() or None
EMBEDDING_DIM = int(os.getenv("EMBEDDING_DIM", "384"))

# Team index name (set by admin, distributed to all team members via get_public_key)
VAULT_INDEX_NAME = os.getenv("VAULT_INDEX_NAME", "").strip() or None

def ensure_vault():
    """
    One-shot startup:
    1. Generate local FHE keys if not present (KeyGenerator)
    2. Connect to enVector Cloud with auto_key_setup=True
       (SDK handles key registration → loading)
    3. Create the team index if it doesn't exist
    """
    import pyenvector as ev

    # Phase 1: local key generation
    enc_key = os.path.join(KEY_SUBDIR, "EncKey.json")
    if not os.path.exists(enc_key):
        logger.info(f"Generating keys in {KEY_SUBDIR}...")
        os.makedirs(KEY_SUBDIR, exist_ok=True)
        keygen = KeyGenerator(key_path=KEY_SUBDIR, key_id=KEY_ID, dim_list=[DIM])
        keygen.generate_keys()
    else:
        logger.info(f"Keys found in {KEY_SUBDIR}")

    # Phase 2: connect to enVector Cloud and register key
    if not ENVECTOR_ENDPOINT or not ENVECTOR_API_KEY:
        logger.warning("ENVECTOR_ENDPOINT/ENVECTOR_API_KEY not set — offline mode, no team index.")
        return

    logger.info(f"Connecting to enVector Cloud ({ENVECTOR_ENDPOINT})...")
    try:
        ev.init(
            address=ENVECTOR_ENDPOINT,
            key_path=KEY_DIR,
            key_id=KEY_ID,
            dim=EMBEDDING_DIM,
            eval_mode="rmp",
            auto_key_setup=True,
            access_token=ENVECTOR_API_KEY,
            query_encryption="plain",
        )
        logger.info("Key registered on enVector Cloud (auto_key_setup).")
    except Exception as e:
        logger.warning(f"auto_key_setup failed (key may already be registered): {e}")
        logger.info("Retrying with auto_key_setup=False...")
        ev.init(
            address=ENVECTOR_ENDPOINT,
            key_path=KEY_DIR,
            key_id=KEY_ID,
            dim=EMBEDDING_DIM,
            eval_mode="rmp",
            auto_key_setup=False,
            access_token=ENVECTOR_API_KEY,
            query_encryption="plain",
        )
        logger.info("Connected to enVector Cloud (auto_key_setup=False).")

    # Phase 3: ensure team index
    if not VAULT_INDEX_NAME:
        return

    try:
        existing = ev.get_index_list()
        existing_names = []
        if hasattr(existing, "indexes"):
            existing_names = [idx.index_name for idx in existing.indexes]
        elif isinstance(existing, (list, tuple)):
            existing_names = [str(idx) for idx in existing]

        if VAULT_INDEX_NAME in existing_names:
            logger.info(f"Team index '{VAULT_INDEX_NAME}' already exists.")
        else:
            ev.create_index(
                index_name=VAULT_INDEX_NAME,
                dim=EMBEDDING_DIM,
                index_params={"index_type": "FLAT"},
                query_encryption="plain",
                metadata_encryption=False,
            )
            logger.info(f"Created team index '{VAULT_INDEX_NAME}' (dim={EMBEDDING_DIM}).")
    except Exception as e:
        logger.error(f"Failed to ensure team index: {e}", exc_info=True)

ensure_vault()
enc_key_path = os.path.join(KEY_SUBDIR, "EncKey.json")
sec_key_path = os.path.join(KEY_SUBDIR, "SecKey.json")
metadata_key_path = os.path.join(KEY_SUBDIR, "MetadataKey.json")

# Initialize shared Cipher instance
cipher = Cipher(enc_key_path=enc_key_path, dim=DIM)

# =============================================================================
# Per-Agent Metadata Key Derivation
# =============================================================================
@functools.lru_cache(maxsize=1)
def _load_master_key() -> bytes:
    """Load MetadataKey as master key for per-agent DEK derivation."""
    from pyenvector.utils.utils import get_key_stream
    return get_key_stream(metadata_key_path)

def derive_agent_key(master_key: bytes, agent_id: str) -> bytes:
    """Derive a 32-byte AES-256 DEK for a specific agent via HMAC-SHA256."""
    return hmac.new(master_key, agent_id.encode('utf-8'), hashlib.sha256).digest()

# =============================================================================
# Authorization
# =============================================================================
_ENV_TOKENS = os.getenv("VAULT_TOKENS", "").strip()
if _ENV_TOKENS:
    VALID_TOKENS = set(filter(None, _ENV_TOKENS.split(",")))
else:
    VALID_TOKENS = {
        "TOKEN-FOR-DEMONSTRATION-PURPOSES-ONLY-DO-NOT-USE-IN-PRODUCTION",
    }
    logger.warning("Using demo tokens. Set VAULT_TOKENS env var for production.")


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

# =============================================================================
# Core Business Logic
# =============================================================================
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
        path = os.path.join(KEY_SUBDIR, filename)
        if os.path.exists(path):
            with open(path, "r") as f:
                bundle[filename] = f.read()
        else:
            # Should not happen if ensure_vault ran
            pass

    # Include team index name and key_id so clients discover them dynamically
    if VAULT_INDEX_NAME:
        bundle["index_name"] = VAULT_INDEX_NAME
    bundle["key_id"] = KEY_ID

    # Per-agent metadata DEK: derived from master key + token-based agent_id
    agent_id = hashlib.sha256(token.encode('utf-8')).hexdigest()[:32]
    master_key = _load_master_key()
    agent_dek = derive_agent_key(master_key, agent_id)
    bundle["agent_id"] = agent_id
    bundle["agent_dek"] = base64.b64encode(agent_dek).decode('ascii')

    return json.dumps(bundle)

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

def _decrypt_metadata_impl(token: str, encrypted_metadata_list: list[str]) -> str:
    """
    Core implementation: Decrypts a list of per-agent AES-encrypted metadata.

    Each item is a JSON string: {"a": "<agent_id>", "c": "<base64_ciphertext>"}.
    Vault derives the agent's DEK from master key + agent_id, then decrypts.

    Args:
        token: Authentication token issued by Vault Admin.
        encrypted_metadata_list: List of JSON-encoded per-agent encrypted blobs.

    Returns:
        JSON string containing the list of decrypted metadata objects.
    """
    validate_token(token)

    if not os.path.exists(metadata_key_path):
        return json.dumps({"error": "MetadataKey not found in Vault"})

    try:
        master_key = _load_master_key()
        results = []
        for blob_str in encrypted_metadata_list:
            try:
                blob = json.loads(blob_str)
                agent_id = blob["a"]
                ct_b64 = blob["c"]
                agent_dek = derive_agent_key(master_key, agent_id)
                decrypted = aes_decrypt_metadata(ct_b64, agent_dek)
            except (json.JSONDecodeError, KeyError):
                # Legacy format: plain base64, decrypt with master metadata key
                decrypted = aes_decrypt_metadata(blob_str, metadata_key_path)
            if isinstance(decrypted, bytes):
                decrypted = decrypted.decode('utf-8')
            results.append(decrypted)
        return json.dumps(results)
    except Exception as e:
        return json.dumps({"error": f"Metadata decryption failed: {str(e)}"})
