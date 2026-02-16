"""
gRPC server for Rune-Vault (Phase 1 dual-stack).

Runs alongside the existing FastMCP HTTP/SSE server.
Delegates to the same _*_impl() pure functions in vault_mcp.py.
"""

import json
import time
import logging
import grpc
from concurrent import futures

from grpc_health.v1 import health_pb2, health_pb2_grpc
from grpc_health.v1.health import HealthServicer

from vault_mcp import (
    _get_public_key_impl,
    _decrypt_scores_impl,
    _decrypt_metadata_impl,
)

from proto import vault_service_pb2 as pb2
from proto import vault_service_pb2_grpc as pb2_grpc

try:
    import monitoring
    MONITORING_AVAILABLE = True
except ImportError:
    MONITORING_AVAILABLE = False

logger = logging.getLogger("rune.vault.grpc")

MAX_MESSAGE_LENGTH = 256 * 1024 * 1024  # 256 MB (EvalKey can be tens of MB)


class VaultServiceServicer(pb2_grpc.VaultServiceServicer):
    """gRPC implementation that delegates to vault_mcp._*_impl() functions."""

    def GetPublicKey(self, request, context):
        start_time = time.time()
        status = "success"
        try:
            result_json = _get_public_key_impl(request.token)
            parsed = json.loads(result_json)
            if isinstance(parsed, dict) and "error" in parsed:
                status = "error"
                return pb2.GetPublicKeyResponse(error=parsed["error"])
            return pb2.GetPublicKeyResponse(key_bundle_json=result_json)
        except ValueError as e:
            # Auth / rate-limit errors from validate_token()
            status = "error"
            context.set_code(grpc.StatusCode.UNAUTHENTICATED)
            context.set_details(str(e))
            return pb2.GetPublicKeyResponse(error=str(e))
        except Exception as e:
            status = "error"
            context.set_code(grpc.StatusCode.INTERNAL)
            context.set_details(str(e))
            return pb2.GetPublicKeyResponse(error=str(e))
        finally:
            if MONITORING_AVAILABLE:
                duration = time.time() - start_time
                monitoring.vault_requests_total.labels(
                    method="get_public_key", endpoint="grpc", status=status
                ).inc()
                monitoring.vault_request_duration.labels(
                    method="get_public_key", endpoint="grpc"
                ).observe(duration)

    def DecryptScores(self, request, context):
        start_time = time.time()
        status = "success"
        try:
            result_json = _decrypt_scores_impl(
                request.token,
                request.encrypted_blob_b64,
                request.top_k,
            )
            parsed = json.loads(result_json)
            if isinstance(parsed, dict) and "error" in parsed:
                status = "error"
                return pb2.DecryptScoresResponse(error=parsed["error"])

            entries = [
                pb2.ScoreEntry(
                    shard_idx=item["shard_idx"],
                    row_idx=item["row_idx"],
                    score=item["score"],
                )
                for item in parsed
            ]
            return pb2.DecryptScoresResponse(results=entries)
        except ValueError as e:
            status = "error"
            context.set_code(grpc.StatusCode.UNAUTHENTICATED)
            context.set_details(str(e))
            return pb2.DecryptScoresResponse(error=str(e))
        except Exception as e:
            status = "error"
            context.set_code(grpc.StatusCode.INTERNAL)
            context.set_details(str(e))
            return pb2.DecryptScoresResponse(error=str(e))
        finally:
            if MONITORING_AVAILABLE:
                duration = time.time() - start_time
                monitoring.vault_requests_total.labels(
                    method="decrypt_scores", endpoint="grpc", status=status
                ).inc()
                monitoring.vault_request_duration.labels(
                    method="decrypt_scores", endpoint="grpc"
                ).observe(duration)

    def DecryptMetadata(self, request, context):
        start_time = time.time()
        status = "success"
        try:
            result_json = _decrypt_metadata_impl(
                request.token,
                list(request.encrypted_metadata_list),
            )
            parsed = json.loads(result_json)
            if isinstance(parsed, dict) and "error" in parsed:
                status = "error"
                return pb2.DecryptMetadataResponse(error=parsed["error"])

            # Each element is a decrypted metadata object.
            # Serialize non-string items back to JSON string for the proto field.
            decrypted_strings = [
                json.dumps(item) if not isinstance(item, str) else item
                for item in parsed
            ]
            return pb2.DecryptMetadataResponse(decrypted_metadata=decrypted_strings)
        except ValueError as e:
            status = "error"
            context.set_code(grpc.StatusCode.UNAUTHENTICATED)
            context.set_details(str(e))
            return pb2.DecryptMetadataResponse(error=str(e))
        except Exception as e:
            status = "error"
            context.set_code(grpc.StatusCode.INTERNAL)
            context.set_details(str(e))
            return pb2.DecryptMetadataResponse(error=str(e))
        finally:
            if MONITORING_AVAILABLE:
                duration = time.time() - start_time
                monitoring.vault_requests_total.labels(
                    method="decrypt_metadata", endpoint="grpc", status=status
                ).inc()
                monitoring.vault_request_duration.labels(
                    method="decrypt_metadata", endpoint="grpc"
                ).observe(duration)


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
    )

    # Register VaultService
    pb2_grpc.add_VaultServiceServicer_to_server(VaultServiceServicer(), server)

    # Register gRPC health checking (standard grpc.health.v1 protocol)
    health_servicer = HealthServicer()
    health_pb2_grpc.add_HealthServicer_to_server(health_servicer, server)
    health_servicer.set(
        "rune.vault.v1.VaultService",
        health_pb2.HealthCheckResponse.SERVING,
    )
    health_servicer.set("", health_pb2.HealthCheckResponse.SERVING)

    server.add_insecure_port(f"{host}:{port}")
    server.start()
    logger.info(f"gRPC server started on {host}:{port}")
    return server
