"""
Unit tests for structured audit logging (issue #19).
"""

import json
import os
import sys
import tempfile
from unittest.mock import MagicMock

import pytest

# Add vault to path
sys.path.insert(0, os.path.join(os.path.dirname(__file__), "../../vault"))

from audit import (
    AuditLogger,
    _parse_audit_config,
    extract_source_ip,
)


# ---------------------------------------------------------------------------
# Config parsing
# ---------------------------------------------------------------------------


class TestParseAuditConfig:
    def test_empty_string(self):
        cfg = _parse_audit_config("")
        assert cfg["file"] is None
        assert cfg["stdout"] is False

    def test_file_default_path(self):
        cfg = _parse_audit_config("file")
        assert cfg["file"] == "/var/log/rune-vault/audit.log"
        assert cfg["stdout"] is False

    def test_file_custom_path(self):
        cfg = _parse_audit_config("file:/tmp/my-audit.log")
        assert cfg["file"] == "/tmp/my-audit.log"

    def test_stdout(self):
        cfg = _parse_audit_config("stdout")
        assert cfg["file"] is None
        assert cfg["stdout"] is True

    def test_file_plus_stdout(self):
        cfg = _parse_audit_config("file+stdout")
        assert cfg["file"] == "/var/log/rune-vault/audit.log"
        assert cfg["stdout"] is True

    def test_stdout_plus_file(self):
        cfg = _parse_audit_config("stdout+file")
        assert cfg["file"] == "/var/log/rune-vault/audit.log"
        assert cfg["stdout"] is True

    def test_custom_path_plus_stdout(self):
        cfg = _parse_audit_config("file:/tmp/my-audit.log+stdout")
        assert cfg["file"] == "/tmp/my-audit.log"
        assert cfg["stdout"] is True

    def test_custom_path_preserves_case(self):
        cfg = _parse_audit_config("file:/var/log/Rune-Vault/Audit.log")
        assert cfg["file"] == "/var/log/Rune-Vault/Audit.log"

    def test_file_keyword_case_insensitive(self):
        cfg = _parse_audit_config("FILE+STDOUT")
        assert cfg["file"] == "/var/log/rune-vault/audit.log"
        assert cfg["stdout"] is True


# ---------------------------------------------------------------------------
# Source IP extraction
# ---------------------------------------------------------------------------


class TestExtractSourceIp:
    def _make_context(self, peer_value):
        ctx = MagicMock()
        ctx.peer.return_value = peer_value
        return ctx

    def test_ipv4(self):
        assert extract_source_ip(self._make_context("ipv4:10.0.0.1:12345")) == "10.0.0.1"

    def test_ipv4_no_port(self):
        # Defensive: if port is missing
        assert extract_source_ip(self._make_context("ipv4:10.0.0.1")) == "10.0.0.1"

    def test_ipv6_bracketed(self):
        assert extract_source_ip(self._make_context("ipv6:[::1]:12345")) == "[::1]"

    def test_ipv6_no_brackets(self):
        result = extract_source_ip(self._make_context("ipv6:::1:12345"))
        assert result  # should not crash

    def test_none_peer(self):
        assert extract_source_ip(self._make_context(None)) == "unknown"

    def test_exception(self):
        ctx = MagicMock()
        ctx.peer.side_effect = RuntimeError("broken")
        assert extract_source_ip(ctx) == "unknown"

    def test_unix_socket(self):
        result = extract_source_ip(self._make_context("unix:/var/run/vault.sock"))
        assert result == "unix:/var/run/vault.sock"


# ---------------------------------------------------------------------------
# AuditLogger
# ---------------------------------------------------------------------------


class TestAuditLogger:
    def test_disabled_when_no_handlers(self):
        logger = AuditLogger({"file": None, "stdout": False})
        assert logger.enabled is False

    def test_file_mode_writes_json(self):
        with tempfile.NamedTemporaryFile(mode="r", suffix=".log", delete=False) as f:
            path = f.name
        try:
            logger = AuditLogger({"file": path, "stdout": False})
            assert logger.enabled is True
            entry = logger.log(
                timestamp="2026-03-30T12:00:00+00:00",
                user_id="alice",
                method="decrypt_scores",
                top_k=10,
                result_count=5,
                status="success",
                source_ip="10.0.0.1",
                latency_ms=42.567,
            )
            # Force flush
            for h in logger._logger.handlers:
                h.flush()
            with open(path) as fh:
                line = fh.readline().strip()
            parsed = json.loads(line)
            assert parsed["user_id"] == "alice"
            assert parsed["method"] == "decrypt_scores"
            assert parsed["top_k"] == 10
            assert parsed["result_count"] == 5
            assert parsed["status"] == "success"
            assert parsed["source_ip"] == "10.0.0.1"
            assert parsed["latency_ms"] == 42.57
            assert parsed["timestamp"] == "2026-03-30T12:00:00+00:00"
            assert "error" not in parsed
            # Verify return value matches
            assert entry["user_id"] == "alice"
        finally:
            os.unlink(path)

    def test_error_field_included(self):
        with tempfile.NamedTemporaryFile(mode="r", suffix=".log", delete=False) as f:
            path = f.name
        try:
            logger = AuditLogger({"file": path, "stdout": False})
            logger.log(
                timestamp="2026-03-30T12:00:00+00:00",
                user_id="unknown",
                method="decrypt_scores",
                top_k=None,
                result_count=0,
                status="error",
                source_ip="10.0.0.99",
                latency_ms=1.23,
                error="Invalid authentication token",
            )
            for h in logger._logger.handlers:
                h.flush()
            with open(path) as fh:
                parsed = json.loads(fh.readline().strip())
            assert parsed["status"] == "error"
            assert parsed["error"] == "Invalid authentication token"
            assert parsed["top_k"] is None
        finally:
            os.unlink(path)

    def test_stdout_mode(self, capsys):
        logger = AuditLogger({"file": None, "stdout": True})
        assert logger.enabled is True
        logger.log(
            timestamp="2026-03-30T12:00:00+00:00",
            user_id="bob",
            method="get_public_key",
            top_k=None,
            result_count=1,
            status="success",
            source_ip="10.0.0.2",
            latency_ms=5.0,
        )
        captured = capsys.readouterr()
        parsed = json.loads(captured.out.strip())
        assert parsed["user_id"] == "bob"

    def test_empty_error_string_included(self):
        with tempfile.NamedTemporaryFile(mode="r", suffix=".log", delete=False) as f:
            path = f.name
        try:
            logger = AuditLogger({"file": path, "stdout": False})
            logger.log(
                timestamp="2026-03-30T12:00:00+00:00",
                user_id="test",
                method="decrypt_scores",
                top_k=None,
                result_count=0,
                status="error",
                source_ip="10.0.0.1",
                latency_ms=1.0,
                error="",
            )
            for h in logger._logger.handlers:
                h.flush()
            with open(path) as fh:
                parsed = json.loads(fh.readline().strip())
            assert "error" in parsed
            assert parsed["error"] == ""
        finally:
            os.unlink(path)

    def test_entry_schema_required_fields(self):
        with tempfile.NamedTemporaryFile(mode="r", suffix=".log", delete=False) as f:
            path = f.name
        try:
            logger = AuditLogger({"file": path, "stdout": False})
            logger.log(
                timestamp="2026-03-30T12:00:00+00:00",
                user_id="test",
                method="decrypt_metadata",
                top_k=None,
                result_count=3,
                status="success",
                source_ip="127.0.0.1",
                latency_ms=10.0,
            )
            for h in logger._logger.handlers:
                h.flush()
            with open(path) as fh:
                parsed = json.loads(fh.readline().strip())
            required = {"timestamp", "user_id", "method", "top_k", "result_count",
                        "status", "source_ip", "latency_ms"}
            assert required.issubset(parsed.keys())
        finally:
            os.unlink(path)


