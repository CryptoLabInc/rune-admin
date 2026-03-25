"""
Rune-Vault Core Business Logic

Pure business logic for FHE key management, authentication, and decryption.
No transport layer (MCP, gRPC) — consumed by vault_grpc_server.py.
"""

import base64
import hashlib
import heapq
import logging
import os
import json

logger = logging.getLogger("rune.vault")
from cryptography.hazmat.primitives.kdf.hkdf import HKDF
from cryptography.hazmat.primitives import hashes
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
        keygen = KeyGenerator(key_path=KEY_SUBDIR, key_id=KEY_ID, dim_list=[DIM], metadata_encryption=False)
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

# Initialize shared Cipher instance
cipher = Cipher(enc_key_path=enc_key_path, dim=DIM)

# =============================================================================
# Per-Agent Metadata Key Derivation (HKDF-SHA256)
# =============================================================================
def derive_agent_key(team_secret: str, agent_id: str) -> bytes:
    """Derive a 32-byte AES-256 DEK for a specific agent via HKDF-SHA256.

    Args:
        team_secret: Team-wide master secret (VAULT_TEAM_SECRET).
        agent_id: Per-user agent identifier derived from token.

    Returns:
        32-byte AES-256 key.
    """
    hkdf = HKDF(
        algorithm=hashes.SHA256(),
        length=32,
        salt=None,
        info=agent_id.encode('utf-8'),
    )
    return hkdf.derive(team_secret.encode('utf-8'))

# =============================================================================
# Authorization (per-user token auth via TokenStore)
# =============================================================================
from token_store import (
    token_store, TokenNotFoundError, TokenExpiredError,
    RateLimitError, TopKExceededError, ScopeError,
)

# Team secret for DEK derivation (shared across all users)
VAULT_TEAM_SECRET = (
    os.getenv("VAULT_TEAM_SECRET", "").strip()
    or os.getenv("VAULT_TOKENS", "").strip()
)

# Load token/role configuration (priority: files > env var > demo)
_roles_path = os.getenv("VAULT_ROLES_FILE", "/app/config/vault-roles.yml")
_tokens_path = os.getenv("VAULT_TOKENS_FILE", "/app/config/vault-tokens.yml")

if os.path.exists(_roles_path) or os.path.exists(_tokens_path):
    token_store.load_from_files(_roles_path, _tokens_path)
    logger.info("Per-user token auth loaded from config files")
elif VAULT_TEAM_SECRET:
    token_store.load_legacy_env(VAULT_TEAM_SECRET)
    logger.warning("Legacy single-token mode. Migrate to per-user tokens via runevault CLI.")
else:
    token_store.load_defaults_with_demo_token()
    logger.warning("Demo mode. Set VAULT_TEAM_SECRET for production.")


def validate_token(token: str) -> tuple[str, object]:
    """Validate per-user token. Returns (username, Role)."""
    return token_store.validate(token)

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
    username, role = validate_token(token)
    token_store.check_scope(role, "get_public_key")

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

    # Per-user metadata DEK: derived from VAULT_TEAM_SECRET + token-based agent_id
    agent_id = hashlib.sha256(token.encode('utf-8')).hexdigest()[:32]
    agent_dek = derive_agent_key(VAULT_TEAM_SECRET, agent_id)
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
    username, role = validate_token(token)
    token_store.check_scope(role, "decrypt_scores")

    # Per-role top_k enforcement
    if top_k > role.top_k:
        raise TopKExceededError(top_k, role.top_k, role.name)

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
    Vault derives the agent's DEK from VAULT_TEAM_SECRET + agent_id via HKDF.

    Args:
        token: Authentication token issued by Vault Admin.
        encrypted_metadata_list: List of JSON-encoded per-agent encrypted blobs.

    Returns:
        JSON string containing the list of decrypted metadata objects.
    """
    username, role = validate_token(token)
    token_store.check_scope(role, "decrypt_metadata")

    if not VAULT_TEAM_SECRET:
        return json.dumps({"error": "VAULT_TEAM_SECRET not configured"})

    try:
        results = []
        for blob_str in encrypted_metadata_list:
            blob = json.loads(blob_str)
            agent_id = blob["a"]
            ct_b64 = blob["c"]
            agent_dek = derive_agent_key(VAULT_TEAM_SECRET, agent_id)
            decrypted = aes_decrypt_metadata(ct_b64, agent_dek)
            if isinstance(decrypted, bytes):
                decrypted = decrypted.decode('utf-8')
            results.append(decrypted)
        return json.dumps(results)
    except Exception as e:
        return json.dumps({"error": f"Metadata decryption failed: {str(e)}"})
