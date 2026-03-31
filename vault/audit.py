"""
Structured audit logging for Rune-Vault operations.

Emits one JSON line per gRPC request to a dedicated audit log,
separate from the application log. Supports file-based daily rotation
and stdout JSON mode for container environments.

Configuration via VAULT_AUDIT_LOG env var:
  (empty)        disabled
  file           /var/log/rune-vault/audit.log, daily rotation, 30-day retention
  file:/path     custom file path
  stdout         JSON lines to stdout
  file+stdout    both
"""

import json
import logging
import os
import sys
from logging.handlers import TimedRotatingFileHandler
from typing import Any

_DEFAULT_AUDIT_PATH = "/var/log/rune-vault/audit.log"

# ---------------------------------------------------------------------------
# Configuration parsing
# ---------------------------------------------------------------------------


def _parse_audit_config(env_value: str) -> dict:
    """Parse VAULT_AUDIT_LOG into {"file": path | None, "stdout": bool}."""
    if not env_value:
        return {"file": None, "stdout": False}

    parts = [p.strip() for p in env_value.split("+")]
    config: dict[str, Any] = {"file": None, "stdout": False}

    for part in parts:
        lowered = part.lower()
        if lowered == "stdout":
            config["stdout"] = True
        elif lowered == "file":
            config["file"] = _DEFAULT_AUDIT_PATH
        elif lowered.startswith("file:"):
            config["file"] = part.split(":", 1)[1].strip()

    return config


# ---------------------------------------------------------------------------
# Source IP extraction
# ---------------------------------------------------------------------------


def extract_source_ip(context) -> str:
    """Extract client IP from gRPC context.peer().

    peer() returns strings like:
      'ipv4:10.0.0.1:12345'
      'ipv6:[::1]:12345'
      'unix:/path/to/socket'
    """
    try:
        peer = context.peer()
        if peer is None:
            return "unknown"
        if peer.startswith("ipv4:"):
            # ipv4:10.0.0.1:12345 -> 10.0.0.1
            return peer[5:].rsplit(":", 1)[0]
        if peer.startswith("ipv6:"):
            addr = peer[5:]
            if addr.startswith("["):
                # [::1]:12345 -> [::1]
                return addr.split("]", 1)[0] + "]"
            return addr.rsplit(":", 1)[0]
        return peer
    except Exception:
        return "unknown"


# ---------------------------------------------------------------------------
# AuditLogger
# ---------------------------------------------------------------------------


class AuditLogger:
    """JSON-structured audit logger with file rotation and stdout support."""

    def __init__(self, config: dict):
        self._logger = logging.getLogger("rune.vault.audit")
        self._logger.setLevel(logging.INFO)
        self._logger.propagate = False

        # Close and remove any pre-existing handlers (e.g. during tests)
        for h in self._logger.handlers[:]:
            h.close()
            self._logger.removeHandler(h)

        if config.get("file"):
            handler = TimedRotatingFileHandler(
                config["file"],
                when="midnight",
                backupCount=30,
                utc=True,
            )
            handler.setFormatter(logging.Formatter("%(message)s"))
            self._logger.addHandler(handler)

        if config.get("stdout"):
            handler = logging.StreamHandler(sys.stdout)
            handler.setFormatter(logging.Formatter("%(message)s"))
            self._logger.addHandler(handler)

    @property
    def enabled(self) -> bool:
        return len(self._logger.handlers) > 0

    def log(
        self,
        *,
        timestamp: str,
        user_id: str,
        method: str,
        top_k: int | None,
        result_count: int,
        status: str,
        source_ip: str,
        latency_ms: float,
        error: str | None = None,
    ) -> dict:
        """Emit a single structured audit entry. Returns the entry dict."""
        entry: dict[str, Any] = {
            "timestamp": timestamp,
            "user_id": user_id,
            "method": method,
            "top_k": top_k,
            "result_count": result_count,
            "status": status,
            "source_ip": source_ip,
            "latency_ms": round(latency_ms, 2),
        }
        if error is not None:
            entry["error"] = error
        self._logger.info(json.dumps(entry, separators=(",", ":")))
        return entry


# ---------------------------------------------------------------------------
# Module-level singleton
# ---------------------------------------------------------------------------

_config = _parse_audit_config(os.environ.get("VAULT_AUDIT_LOG", ""))
audit_logger = AuditLogger(_config)
