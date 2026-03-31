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

logger = logging.getLogger("rune.vault.validation")

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
                context.abort(grpc.StatusCode.INVALID_ARGUMENT, msg)
                return None
            except RuntimeValidationError as exc:
                logger.warning("Validation rejected %s: %s", method, exc)
                context.abort(grpc.StatusCode.INVALID_ARGUMENT, str(exc))
                return None
            return original_handler(request, context)

        return grpc.unary_unary_rpc_method_handler(
            validating_handler,
            request_deserializer=next_handler.request_deserializer,
            response_serializer=next_handler.response_serializer,
        )

