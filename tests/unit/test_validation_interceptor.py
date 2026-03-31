"""
Unit tests for the gRPC ValidationInterceptor.

Tests the interceptor wiring using mock objects — no real gRPC server needed.
grpc and protovalidate are mocked to avoid heavy runtime dependencies.
"""
import pytest
import sys
import os
from unittest.mock import MagicMock, patch, PropertyMock
from types import ModuleType

# ---------------------------------------------------------------------------
# Mock heavy dependencies before importing vault modules
# ---------------------------------------------------------------------------

_grpc_mock = ModuleType("grpc")
_grpc_mock.ServerInterceptor = type("ServerInterceptor", (), {})
_grpc_mock.StatusCode = type("StatusCode", (), {
    "INVALID_ARGUMENT": "INVALID_ARGUMENT",
})()
_grpc_mock.unary_unary_rpc_method_handler = lambda handler, **kw: MagicMock(
    unary_unary=handler
)
sys.modules.setdefault("grpc", _grpc_mock)

# Force-mock protovalidate regardless of prior imports — prevents test
# isolation failures when the full suite loads the real module first.
_protovalidate_mock = ModuleType("protovalidate")


class _ValidationError(Exception):
    def __init__(self, msg="validation error", violations=None):
        self.violations = violations or []
        super().__init__(msg)


_protovalidate_mock.ValidationError = _ValidationError
_protovalidate_mock.Validator = MagicMock
sys.modules["protovalidate"] = _protovalidate_mock

sys.path.insert(0, os.path.join(os.path.dirname(__file__), '../../vault'))

from request_validator import RuntimeValidationError
from validation_interceptor import ValidationInterceptor

# Get the actual ValidationError the interceptor will catch
_ProtoValidationError = sys.modules["protovalidate"].ValidationError


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _make_handler_call_details(method: str):
    details = MagicMock()
    details.method = method
    return details


def _make_next_handler(return_value="ok"):
    handler = MagicMock()
    handler.unary_unary = MagicMock(return_value=return_value)
    handler.request_deserializer = None
    handler.response_serializer = None
    return handler


def _make_context():
    ctx = MagicMock()
    ctx.abort = MagicMock(side_effect=Exception("aborted"))
    return ctx


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------

class TestValidationInterceptor:
    def setup_method(self):
        self.interceptor = ValidationInterceptor()

    def test_non_vault_method_passes_through(self):
        """Health check and other non-vault methods bypass validation."""
        details = _make_handler_call_details("/grpc.health.v1.Health/Check")
        next_handler = _make_next_handler()
        continuation = MagicMock(return_value=next_handler)

        result = self.interceptor.intercept_service(continuation, details)
        assert result is next_handler

    def test_none_handler_returns_none(self):
        details = _make_handler_call_details("/rune.vault.v1.VaultService/GetPublicKey")
        continuation = MagicMock(return_value=None)

        result = self.interceptor.intercept_service(continuation, details)
        assert result is None

    def test_valid_request_reaches_handler(self):
        """A valid request passes both validation layers."""
        details = _make_handler_call_details("/rune.vault.v1.VaultService/GetPublicKey")
        next_handler = _make_next_handler(return_value="response")
        continuation = MagicMock(return_value=next_handler)

        wrapped = self.interceptor.intercept_service(continuation, details)

        request = MagicMock()
        request.token = "valid-token-123"
        context = _make_context()

        with patch("validation_interceptor.validate_proto"):
            result = wrapped.unary_unary(request, context)

        assert result == "response"
        context.abort.assert_not_called()

    def test_proto_validation_error_aborts(self):
        """protovalidate.ValidationError triggers INVALID_ARGUMENT abort."""
        details = _make_handler_call_details("/rune.vault.v1.VaultService/DecryptScores")
        next_handler = _make_next_handler()
        continuation = MagicMock(return_value=next_handler)

        wrapped = self.interceptor.intercept_service(continuation, details)

        request = MagicMock()
        request.token = "valid-token"
        context = _make_context()

        violation = MagicMock()
        violation.proto.field = "top_k"
        violation.proto.message = "value must be >= 1"
        exc = _ProtoValidationError(violations=[violation])

        with patch("validation_interceptor.validate_proto", side_effect=exc):
            with pytest.raises(Exception, match="aborted"):
                wrapped.unary_unary(request, context)

        context.abort.assert_called_once()
        status_code = context.abort.call_args[0][0]
        assert "INVALID_ARGUMENT" in str(status_code)
        assert "top_k" in context.abort.call_args[0][1]

    def test_runtime_validation_error_aborts(self):
        """RuntimeValidationError (control chars) triggers INVALID_ARGUMENT abort."""
        details = _make_handler_call_details("/rune.vault.v1.VaultService/GetPublicKey")
        next_handler = _make_next_handler()
        continuation = MagicMock(return_value=next_handler)

        wrapped = self.interceptor.intercept_service(continuation, details)

        request = MagicMock()
        request.token = "tok\x00en"
        context = _make_context()

        with patch("validation_interceptor.validate_proto"):
            with pytest.raises(Exception, match="aborted"):
                wrapped.unary_unary(request, context)

        context.abort.assert_called_once()
        status_code = context.abort.call_args[0][0]
        assert "INVALID_ARGUMENT" in str(status_code)
        assert "control characters" in context.abort.call_args[0][1]

    def test_handler_without_unary_unary_passes_through(self):
        details = _make_handler_call_details("/rune.vault.v1.VaultService/GetPublicKey")
        next_handler = MagicMock()
        next_handler.unary_unary = None
        continuation = MagicMock(return_value=next_handler)

        result = self.interceptor.intercept_service(continuation, details)
        assert result is next_handler

    def test_error_detail_is_human_readable(self):
        """Validation errors include field path and message."""
        details = _make_handler_call_details("/rune.vault.v1.VaultService/DecryptScores")
        next_handler = _make_next_handler()
        continuation = MagicMock(return_value=next_handler)

        wrapped = self.interceptor.intercept_service(continuation, details)

        request = MagicMock()
        request.token = "valid-token"
        context = _make_context()

        violation = MagicMock()
        violation.proto.field = "encrypted_blob_b64"
        violation.proto.message = "value length must be at least 1"
        exc = _ProtoValidationError(violations=[violation])

        with patch("validation_interceptor.validate_proto", side_effect=exc):
            with pytest.raises(Exception, match="aborted"):
                wrapped.unary_unary(request, context)

        detail_msg = context.abort.call_args[0][1]
        assert "encrypted_blob_b64" in detail_msg
        assert "at least 1" in detail_msg
