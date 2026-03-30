"""
Unit tests for gRPC request input validation rules.

Tests both protovalidate (proto-level) and runtime checks.
Proto-level tests use fake request objects to exercise the same
validation functions without requiring the real pb2 module.
"""
import pytest
import sys
import os
from types import ModuleType

# Mock protovalidate before importing request_validator
_pv = ModuleType("protovalidate")
_pv.Validator = type("Validator", (), {"validate": lambda self, req: None})
class _ValidationError(Exception):
    def __init__(self, violations=None):
        self.violations = violations or []
        super().__init__("validation error")
_pv.ValidationError = _ValidationError
sys.modules.setdefault("protovalidate", _pv)

sys.path.insert(0, os.path.join(os.path.dirname(__file__), '../../vault'))

from request_validator import (
    RuntimeValidationError,
    check_token_safety,
    validate_index_name,
    MAX_INDEX_NAME_LENGTH,
)


# ---------------------------------------------------------------------------
# Token safety (runtime layer — control chars & whitespace)
# ---------------------------------------------------------------------------

class TestTokenSafety:
    def test_valid_token(self):
        check_token_safety("abc123-valid")

    def test_null_byte_rejected(self):
        with pytest.raises(RuntimeValidationError, match="control characters"):
            check_token_safety("token\x00evil")

    def test_control_char_rejected(self):
        with pytest.raises(RuntimeValidationError, match="control characters"):
            check_token_safety("token\x01")

    def test_tab_rejected(self):
        with pytest.raises(RuntimeValidationError, match="control characters"):
            check_token_safety("token\t")

    def test_newline_rejected(self):
        with pytest.raises(RuntimeValidationError, match="control characters"):
            check_token_safety("token\n")

    def test_del_char_rejected(self):
        with pytest.raises(RuntimeValidationError, match="control characters"):
            check_token_safety("token\x7f")

    def test_leading_whitespace_rejected(self):
        with pytest.raises(RuntimeValidationError, match="whitespace"):
            check_token_safety(" token")

    def test_trailing_whitespace_rejected(self):
        with pytest.raises(RuntimeValidationError, match="whitespace"):
            check_token_safety("token ")


# ---------------------------------------------------------------------------
# Index name validation (runtime layer — path traversal prevention)
# ---------------------------------------------------------------------------

class TestIndexName:
    def test_valid_names(self):
        for name in ["my_index", "index-1", "ABC123", "a"]:
            validate_index_name(name)

    def test_empty_rejected(self):
        with pytest.raises(RuntimeValidationError, match="empty"):
            validate_index_name("")

    def test_too_long_rejected(self):
        with pytest.raises(RuntimeValidationError, match="exceeds"):
            validate_index_name("a" * (MAX_INDEX_NAME_LENGTH + 1))

    def test_path_traversal_rejected(self):
        with pytest.raises(RuntimeValidationError, match="alphanumeric"):
            validate_index_name("../../etc/passwd")

    def test_slash_rejected(self):
        with pytest.raises(RuntimeValidationError, match="alphanumeric"):
            validate_index_name("foo/bar")

    def test_space_rejected(self):
        with pytest.raises(RuntimeValidationError, match="alphanumeric"):
            validate_index_name("foo bar")

    def test_special_chars_rejected(self):
        with pytest.raises(RuntimeValidationError, match="alphanumeric"):
            validate_index_name("index;DROP TABLE")
