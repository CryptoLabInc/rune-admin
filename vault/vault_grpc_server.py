"""
gRPC server for Rune-Vault.

Sole entry point for the Vault service.
Delegates to _*_impl() pure functions in vault_core.py.
"""

import json
import os
import signal
import time
import logging
from datetime import datetime, timezone

import grpc
from concurrent import futures

from grpc_health.v1 import health_pb2, health_pb2_grpc
from grpc_health.v1.health import HealthServicer
from grpc_reflection.v1alpha import reflection

from vault_core import (
    _get_public_key_impl,
    _decrypt_scores_impl,
    _decrypt_metadata_impl,
    token_store,
)
from token_store import (
    TokenNotFoundError, TokenExpiredError,
    RateLimitError, TopKExceededError, ScopeError,
)
from admin_server import start_admin_server
from validation_interceptor import ValidationInterceptor

from proto import vault_service_pb2 as pb2
from proto import vault_service_pb2_grpc as pb2_grpc

try:
    from audit import audit_logger, extract_source_ip
    AUDIT_AVAILABLE = True
except ImportError:
    AUDIT_AVAILABLE = False

logger = logging.getLogger("rune.vault.grpc")

MAX_MESSAGE_LENGTH = 256 * 1024 * 1024  # 256 MB (EvalKey can be tens of MB)


def _emit_audit(method, user, top_k, result_count, status, error_detail,
                 duration, context):
    """Emit audit log entry."""
    if not (AUDIT_AVAILABLE and audit_logger.enabled):
        return
    audit_logger.log(
        timestamp=datetime.now(timezone.utc).isoformat(),
        user_id=user,
        method=method,
        top_k=top_k,
        result_count=result_count,
        status=status,
        source_ip=extract_source_ip(context),
        latency_ms=duration * 1000,
        error=error_detail,
    )


class VaultServiceServicer(pb2_grpc.VaultServiceServicer):
    """gRPC implementation that delegates to vault_core._*_impl() functions."""

    def GetPublicKey(self, request, context):
        start_time = time.time()
        status = "success"
        user = "unknown"
        result_count = 0
        error_detail = None
        try:
            user = token_store.get_username(request.token) or "unknown"
            result_json = _get_public_key_impl(request.token)
            parsed = json.loads(result_json)
            if isinstance(parsed, dict) and "error" in parsed:
                status = "error"
                error_detail = parsed["error"]
                return pb2.GetPublicKeyResponse(error=parsed["error"])
            result_count = 1
            return pb2.GetPublicKeyResponse(key_bundle_json=result_json)
        except (TokenNotFoundError, TokenExpiredError) as e:
            status = "denied"
            error_detail = str(e)
            context.set_code(grpc.StatusCode.UNAUTHENTICATED)
            context.set_details(str(e))
            return pb2.GetPublicKeyResponse(error=str(e))
        except RateLimitError as e:
            status = "denied"
            error_detail = str(e)
            context.set_code(grpc.StatusCode.RESOURCE_EXHAUSTED)
            context.set_details(str(e))
            return pb2.GetPublicKeyResponse(error=str(e))
        except ScopeError as e:
            status = "denied"
            error_detail = str(e)
            context.set_code(grpc.StatusCode.PERMISSION_DENIED)
            context.set_details(str(e))
            return pb2.GetPublicKeyResponse(error=str(e))
        except ValueError as e:
            status = "denied"
            error_detail = str(e)
            context.set_code(grpc.StatusCode.UNAUTHENTICATED)
            context.set_details(str(e))
            return pb2.GetPublicKeyResponse(error=str(e))
        except Exception as e:
            status = "error"
            error_detail = str(e)
            context.set_code(grpc.StatusCode.INTERNAL)
            context.set_details(str(e))
            return pb2.GetPublicKeyResponse(error=str(e))
        finally:
            duration = time.time() - start_time
            _emit_audit("get_public_key", user, None, result_count,
                        status, error_detail, duration, context)

    def DecryptScores(self, request, context):
        start_time = time.time()
        status = "success"
        user = "unknown"
        result_count = 0
        error_detail = None
        try:
            user = token_store.get_username(request.token) or "unknown"
            result_json = _decrypt_scores_impl(
                request.token,
                request.encrypted_blob_b64,
                request.top_k,
            )
            parsed = json.loads(result_json)
            if isinstance(parsed, dict) and "error" in parsed:
                status = "error"
                error_detail = parsed["error"]
                return pb2.DecryptScoresResponse(error=parsed["error"])

            entries = [
                pb2.ScoreEntry(
                    shard_idx=item["shard_idx"],
                    row_idx=item["row_idx"],
                    score=item["score"],
                )
                for item in parsed
            ]
            result_count = len(entries)
            return pb2.DecryptScoresResponse(results=entries)
        except TopKExceededError as e:
            status = "denied"
            error_detail = str(e)
            context.set_code(grpc.StatusCode.INVALID_ARGUMENT)
            context.set_details(str(e))
            return pb2.DecryptScoresResponse(error=str(e))
        except (TokenNotFoundError, TokenExpiredError) as e:
            status = "denied"
            error_detail = str(e)
            context.set_code(grpc.StatusCode.UNAUTHENTICATED)
            context.set_details(str(e))
            return pb2.DecryptScoresResponse(error=str(e))
        except RateLimitError as e:
            status = "denied"
            error_detail = str(e)
            context.set_code(grpc.StatusCode.RESOURCE_EXHAUSTED)
            context.set_details(str(e))
            return pb2.DecryptScoresResponse(error=str(e))
        except ScopeError as e:
            status = "denied"
            error_detail = str(e)
            context.set_code(grpc.StatusCode.PERMISSION_DENIED)
            context.set_details(str(e))
            return pb2.DecryptScoresResponse(error=str(e))
        except ValueError as e:
            status = "denied"
            error_detail = str(e)
            context.set_code(grpc.StatusCode.UNAUTHENTICATED)
            context.set_details(str(e))
            return pb2.DecryptScoresResponse(error=str(e))
        except Exception as e:
            status = "error"
            error_detail = str(e)
            context.set_code(grpc.StatusCode.INTERNAL)
            context.set_details(str(e))
            return pb2.DecryptScoresResponse(error=str(e))
        finally:
            duration = time.time() - start_time
            _emit_audit("decrypt_scores", user, request.top_k, result_count,
                        status, error_detail, duration, context)

    def DecryptMetadata(self, request, context):
        start_time = time.time()
        status = "success"
        user = "unknown"
        result_count = 0
        error_detail = None
        try:
            user = token_store.get_username(request.token) or "unknown"
            result_json = _decrypt_metadata_impl(
                request.token,
                list(request.encrypted_metadata_list),
            )
            parsed = json.loads(result_json)
            if isinstance(parsed, dict) and "error" in parsed:
                status = "error"
                error_detail = parsed["error"]
                return pb2.DecryptMetadataResponse(error=parsed["error"])

            # Each element is a decrypted metadata object.
            # Serialize non-string items back to JSON string for the proto field.
            decrypted_strings = [
                json.dumps(item) if not isinstance(item, str) else item
                for item in parsed
            ]
            result_count = len(decrypted_strings)
            return pb2.DecryptMetadataResponse(decrypted_metadata=decrypted_strings)
        except (TokenNotFoundError, TokenExpiredError) as e:
            status = "denied"
            error_detail = str(e)
            context.set_code(grpc.StatusCode.UNAUTHENTICATED)
            context.set_details(str(e))
            return pb2.DecryptMetadataResponse(error=str(e))
        except RateLimitError as e:
            status = "denied"
            error_detail = str(e)
            context.set_code(grpc.StatusCode.RESOURCE_EXHAUSTED)
            context.set_details(str(e))
            return pb2.DecryptMetadataResponse(error=str(e))
        except ScopeError as e:
            status = "denied"
            error_detail = str(e)
            context.set_code(grpc.StatusCode.PERMISSION_DENIED)
            context.set_details(str(e))
            return pb2.DecryptMetadataResponse(error=str(e))
        except ValueError as e:
            status = "denied"
            error_detail = str(e)
            context.set_code(grpc.StatusCode.UNAUTHENTICATED)
            context.set_details(str(e))
            return pb2.DecryptMetadataResponse(error=str(e))
        except Exception as e:
            status = "error"
            error_detail = str(e)
            context.set_code(grpc.StatusCode.INTERNAL)
            context.set_details(str(e))
            return pb2.DecryptMetadataResponse(error=str(e))
        finally:
            duration = time.time() - start_time
            _emit_audit("decrypt_metadata", user, None, result_count,
                        status, error_detail, duration, context)


def _load_tls_credentials():
    """
    Load TLS credentials from environment variables.

    Returns grpc.ServerCredentials or None (if TLS disabled).
    Raises SystemExit if TLS is required but cert/key not provided.
    """
    if os.environ.get("VAULT_TLS_DISABLE", "").lower() == "true":
        logger.warning(
            "TLS DISABLED — gRPC traffic is unencrypted. "
            "Do not use in production."
        )
        return None

    cert_path = os.environ.get("VAULT_TLS_CERT")
    key_path = os.environ.get("VAULT_TLS_KEY")

    if not cert_path or not key_path:
        logger.error(
            "TLS certificate not configured. "
            "Set VAULT_TLS_CERT and VAULT_TLS_KEY, "
            "or set VAULT_TLS_DISABLE=true for insecure mode."
        )
        raise SystemExit(1)

    with open(cert_path, "rb") as f:
        cert_pem = f.read()
    with open(key_path, "rb") as f:
        key_pem = f.read()

    logger.info("TLS configured — cert=%s", cert_path)
    return grpc.ssl_server_credentials([(key_pem, cert_pem)])


def serve_grpc(host: str = "0.0.0.0", port: int = 50051) -> grpc.Server:
    """
    Start the gRPC server. Non-blocking — returns the server object.
    Call server.stop(grace=N) for graceful shutdown.
    """
    server = grpc.server(
        futures.ThreadPoolExecutor(max_workers=4),
        options=[
            ("grpc.max_send_message_length", MAX_MESSAGE_LENGTH),
            ("grpc.max_receive_message_length", MAX_MESSAGE_LENGTH),
        ],
        interceptors=[ValidationInterceptor()],
    )

    # Register VaultService
    pb2_grpc.add_VaultServiceServicer_to_server(VaultServiceServicer(), server)

    # Enable gRPC server reflection (for grpcurl, etc.)
    SERVICE_NAMES = (
        pb2.DESCRIPTOR.services_by_name["VaultService"].full_name,
        reflection.SERVICE_NAME,
    )
    reflection.enable_server_reflection(SERVICE_NAMES, server)

    # Register gRPC health checking (standard grpc.health.v1 protocol)
    health_servicer = HealthServicer()
    health_pb2_grpc.add_HealthServicer_to_server(health_servicer, server)
    health_servicer.set(
        "rune.vault.v1.VaultService",
        health_pb2.HealthCheckResponse.SERVING,
    )
    health_servicer.set("", health_pb2.HealthCheckResponse.SERVING)

    addr = f"{host}:{port}"
    credentials = _load_tls_credentials()
    if credentials:
        server.add_secure_port(addr, credentials)
        logger.info("gRPC server started on %s (TLS)", addr)
    else:
        server.add_insecure_port(addr)
        logger.info("gRPC server started on %s (insecure)", addr)

    server.start()
    return server


if __name__ == "__main__":
    import argparse

    logging.basicConfig(
        level=logging.INFO,
        format="%(asctime)s %(name)s %(levelname)s %(message)s",
    )

    parser = argparse.ArgumentParser(description="Run the Rune-Vault gRPC server.")
    parser.add_argument("--host", default="0.0.0.0", help="Host to bind")
    parser.add_argument("--grpc-port", type=int, default=50051, help="gRPC port")
    args = parser.parse_args()

    # Start admin HTTP server (internal HTTP, not exposed via Docker)
    admin_srv = start_admin_server(token_store)

    # Start gRPC server (non-blocking)
    grpc_server = serve_grpc(host=args.host, port=args.grpc_port)

    # Graceful shutdown on SIGTERM / SIGINT
    def _shutdown(signum, frame):
        logger.info("Received shutdown signal, stopping...")
        admin_srv.shutdown()
        grpc_server.stop(grace=5)

    signal.signal(signal.SIGTERM, _shutdown)
    signal.signal(signal.SIGINT, _shutdown)

    logger.info("Rune-Vault is ready.")
    grpc_server.wait_for_termination()
