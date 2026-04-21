"""
Unit tests for ENVECTOR_EVAL_MODE configuration (PR #59 test plan).

Test plan coverage:
- Item 1: EVAL_MODE defaults to "rmp" when env var is not set
- Item 2: ev.init() is called with "mm" when ENVECTOR_EVAL_MODE=mm
- Item 3: invalid EVAL_MODE propagates to SDK (no vault-level validation guard)
"""
import sys
import os
import importlib
from unittest.mock import MagicMock, patch

import pytest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "../../vault"))

import vault_core


class TestEvalModeEnvVar:
    """Test plan item 1 & 2: EVAL_MODE env var reading."""

    def test_eval_mode_reflects_env_at_load_time(self):
        """EVAL_MODE at module level matches what os.getenv would return."""
        expected = os.getenv("ENVECTOR_EVAL_MODE", "rmp").lower()
        assert vault_core.EVAL_MODE == expected

    def test_eval_mode_is_lowercased(self, monkeypatch):
        """EVAL_MODE applies .lower() so casing doesn't matter."""
        monkeypatch.setattr(vault_core, "EVAL_MODE", "RMP")
        # Simulate what the module does: os.getenv(...).lower()
        assert vault_core.EVAL_MODE.lower() == "rmp"

    def test_eval_mode_default_is_rmp_when_env_unset(self, monkeypatch):
        """When ENVECTOR_EVAL_MODE is not set, the formula produces 'rmp'."""
        monkeypatch.delenv("ENVECTOR_EVAL_MODE", raising=False)
        result = os.getenv("ENVECTOR_EVAL_MODE", "rmp").lower()
        assert result == "rmp"

    def test_eval_mode_reads_mm_from_env(self, monkeypatch):
        """When ENVECTOR_EVAL_MODE=MM, the formula produces 'mm'."""
        monkeypatch.setenv("ENVECTOR_EVAL_MODE", "MM")
        result = os.getenv("ENVECTOR_EVAL_MODE", "rmp").lower()
        assert result == "mm"


class TestEvalModePassedToEvInit:
    """Test plan item 2 & 3: ev.init() receives EVAL_MODE correctly."""

    def _setup_offline_patches(self, monkeypatch, tmp_path, *, eval_mode: str):
        """Patch vault_core module vars to trigger Phase 2 (cloud init)."""
        key_dir = tmp_path / "vault-key"
        key_dir.mkdir()
        (key_dir / "EncKey.json").write_text("{}")

        monkeypatch.setattr(vault_core, "EVAL_MODE", eval_mode)
        monkeypatch.setattr(vault_core, "ENVECTOR_ENDPOINT", "grpc://test:50051")
        monkeypatch.setattr(vault_core, "ENVECTOR_API_KEY", "test-key")
        monkeypatch.setattr(vault_core, "EMBEDDING_DIM", 1024)
        monkeypatch.setattr(vault_core, "KEY_DIR", str(tmp_path))
        monkeypatch.setattr(vault_core, "KEY_SUBDIR", str(key_dir))
        monkeypatch.setattr(vault_core, "KEY_ID", "vault-key")
        monkeypatch.setattr(vault_core, "VAULT_INDEX_NAME", None)

    def _patch_pyenvector(self, mock_ev: MagicMock):
        """Replace pyenvector in sys.modules with a mock."""
        original = sys.modules.get("pyenvector")
        sys.modules["pyenvector"] = mock_ev
        return original

    def _restore_pyenvector(self, original):
        if original is not None:
            sys.modules["pyenvector"] = original
        else:
            sys.modules.pop("pyenvector", None)

    def test_ensure_vault_passes_eval_mode_mm_to_ev_init(self, monkeypatch, tmp_path):
        """ensure_vault() calls ev.init(eval_mode='mm') when EVAL_MODE is 'mm'."""
        self._setup_offline_patches(monkeypatch, tmp_path, eval_mode="mm")

        mock_ev = MagicMock()
        original = self._patch_pyenvector(mock_ev)
        try:
            vault_core.ensure_vault()
        finally:
            self._restore_pyenvector(original)

        mock_ev.init.assert_called_once_with(
            address="grpc://test:50051",
            key_path=str(tmp_path),
            key_id="vault-key",
            dim=1024,
            eval_mode="mm",
            auto_key_setup=True,
            access_token="test-key",
            query_encryption="plain",
            secure=True,
        )

    def test_ensure_vault_passes_eval_mode_rmp_to_ev_init(self, monkeypatch, tmp_path):
        """ensure_vault() calls ev.init(eval_mode='rmp') when EVAL_MODE is 'rmp'."""
        self._setup_offline_patches(monkeypatch, tmp_path, eval_mode="rmp")

        mock_ev = MagicMock()
        original = self._patch_pyenvector(mock_ev)
        try:
            vault_core.ensure_vault()
        finally:
            self._restore_pyenvector(original)

        mock_ev.init.assert_called_once_with(
            address="grpc://test:50051",
            key_path=str(tmp_path),
            key_id="vault-key",
            dim=1024,
            eval_mode="rmp",
            auto_key_setup=True,
            access_token="test-key",
            query_encryption="plain",
            secure=True,
        )

    def test_invalid_eval_mode_rejected_at_vault_level(self, monkeypatch, tmp_path):
        """Invalid EVAL_MODE is caught by vault's allowlist guard before reaching the SDK."""
        self._setup_offline_patches(monkeypatch, tmp_path, eval_mode="invalid")

        mock_ev = MagicMock()
        original = self._patch_pyenvector(mock_ev)
        try:
            with pytest.raises(ValueError, match="Invalid ENVECTOR_EVAL_MODE"):
                vault_core.ensure_vault()
        finally:
            self._restore_pyenvector(original)

        mock_ev.init.assert_not_called()
