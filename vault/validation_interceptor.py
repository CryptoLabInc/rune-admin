"""
gRPC server interceptor that validates request fields before processing.

Runs two validation layers:
  1. protovalidate — enforces .proto annotation constraints
  2. Runtime checks — control chars, whitespace (not expressible in proto)

Rejects malformed requests with INVALID_ARGUMENT before they reach
VaultServiceServicer methods. Non-vault methods (health, reflection)
pass through untouched.
"""

import logging

import grpc
import protovalidate

from request_validator import (
    RUNTIME_CHECKS,
    RuntimeValidationError,
    validate_proto,
)

try:
    import monitoring
    MONITORING_AVAILABLE = True
except ImportError:
    MONITORING_AVAILABLE = False

logger = logging.getLogger("rune.vault.validation")

_METHOD_SHORT_NAMES = {
    "/rune.vault.v1.VaultService/GetPublicKey": "get_public_key",
    "/rune.vault.v1.VaultService/DecryptScores": "decrypt_scores",
    "/rune.vault.v1.VaultService/DecryptMetadata": "decrypt_metadata",
}


class ValidationInterceptor(grpc.ServerInterceptor):
    """Intercepts unary-unary gRPC calls to validate request fields."""

    def intercept_service(self, continuation, handler_call_details):
        method = handler_call_details.method
        next_handler = continuation(handler_call_details)

        if next_handler is None:
            return None

        runtime_check = RUNTIME_CHECKS.get(method)
        if runtime_check is None:
            return next_handler

        original_handler = next_handler.unary_unary
        if original_handler is None:
            return next_handler

        def validating_handler(request, context):
            try:
                # Layer 1: proto annotation constraints
                validate_proto(request)
                # Layer 2: supplementary runtime checks
                runtime_check(request)
            except protovalidate.ValidationError as exc:
                msg = "; ".join(
                    f"{v.proto.field}: {v.proto.message}"
                    for v in exc.violations
                )
                logger.warning("Validation rejected %s: %s", method, msg)
                self._record_metric(method)
                context.abort(grpc.StatusCode.INVALID_ARGUMENT, msg)
            except RuntimeValidationError as exc:
                logger.warning("Validation rejected %s: %s", method, exc)
                self._record_metric(method)
                context.abort(grpc.StatusCode.INVALID_ARGUMENT, str(exc))
            return original_handler(request, context)

        return grpc.unary_unary_rpc_method_handler(
            validating_handler,
            request_deserializer=next_handler.request_deserializer,
            response_serializer=next_handler.response_serializer,
        )

    @staticmethod
    def _record_metric(method: str) -> None:
        if MONITORING_AVAILABLE:
            short = _METHOD_SHORT_NAMES.get(method, method)
            monitoring.vault_requests_total.labels(
                method=short,
                endpoint="grpc",
                status="validation_error",
                user="unknown",
            ).inc()
