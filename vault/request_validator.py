"""
gRPC request input validation for Rune-Vault.

Two layers of validation:
  1. Proto-level: protovalidate enforces constraints declared in .proto
     annotations (field length, int range, repeated item rules).
  2. Runtime: supplementary checks that cannot be expressed in proto
     annotations (control characters, whitespace, path traversal).

Both layers are executed by ValidationInterceptor before requests
reach business logic.
"""

import re

import protovalidate

# ---------------------------------------------------------------------------
# Shared constants & patterns
# ---------------------------------------------------------------------------

MAX_INDEX_NAME_LENGTH = 128
INDEX_NAME_PATTERN = re.compile(r"^[a-zA-Z0-9_-]+$")

_CONTROL_CHAR_RE = re.compile(r"[\x00-\x1f]")

# Cached validator instance (compiles CEL rules once per descriptor).
_validator = protovalidate.Validator()


# ---------------------------------------------------------------------------
# Proto-level validation (protovalidate)
# ---------------------------------------------------------------------------

def validate_proto(request) -> None:
    """Run protovalidate against the request message.

    Raises protovalidate.ValidationError with structured Violation list.
    """
    _validator.validate(request)


# ---------------------------------------------------------------------------
# Supplementary runtime checks
# ---------------------------------------------------------------------------

class RuntimeValidationError(Exception):
    """Raised for checks that proto annotations cannot express."""


def check_token_safety(token: str) -> None:
    """Reject tokens with control characters or surrounding whitespace."""
    if _CONTROL_CHAR_RE.search(token):
        raise RuntimeValidationError(
            "token: must not contain control characters"
        )
    if token != token.strip():
        raise RuntimeValidationError(
            "token: must not have leading or trailing whitespace"
        )


def validate_index_name(name: str) -> None:
    """Validate an index name (for future use)."""
    if not name:
        raise RuntimeValidationError("index_name: must not be empty")
    if len(name) > MAX_INDEX_NAME_LENGTH:
        raise RuntimeValidationError(
            f"index_name: length {len(name)} exceeds maximum "
            f"{MAX_INDEX_NAME_LENGTH}"
        )
    if not INDEX_NAME_PATTERN.match(name):
        raise RuntimeValidationError(
            "index_name: must contain only alphanumeric characters, "
            "underscores, or hyphens"
        )


# ---------------------------------------------------------------------------
# Vault-method supplementary checks (keyed by gRPC method path)
# ---------------------------------------------------------------------------

def _check_get_public_key(request) -> None:
    check_token_safety(request.token)


def _check_decrypt_scores(request) -> None:
    check_token_safety(request.token)


def _check_decrypt_metadata(request) -> None:
    check_token_safety(request.token)


RUNTIME_CHECKS = {
    "/rune.vault.v1.VaultService/GetPublicKey": _check_get_public_key,
    "/rune.vault.v1.VaultService/DecryptScores": _check_decrypt_scores,
    "/rune.vault.v1.VaultService/DecryptMetadata": _check_decrypt_metadata,
}
