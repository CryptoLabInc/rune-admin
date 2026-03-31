"""
Per-user token and role management with async YAML persistence.

Memory-first architecture: changes take effect immediately in-memory,
then async-persist to YAML files for startup recovery.
"""

import datetime
import logging
import os
import secrets
import tempfile
import threading
import time
from collections import defaultdict
from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass

import yaml

logger = logging.getLogger("vault.token_store")


# =============================================================================
# Data Classes
# =============================================================================


@dataclass
class Role:
    """Role definition with scope, top_k, and rate limit."""

    name: str
    scope: list[str]
    top_k: int
    rate_limit: str  # e.g. "30/60s"

    @property
    def rate_limit_parsed(self) -> tuple[int, int]:
        """Parse rate_limit string into (max_requests, window_seconds)."""
        parts = self.rate_limit.replace("s", "").split("/")
        return int(parts[0]), int(parts[1])


@dataclass
class Token:
    """Per-user token with role assignment and expiry."""

    user: str
    token: str
    role: str
    issued_at: str  # ISO date, e.g. "2026-03-20"
    expires: str | None = None  # ISO date or None (never expires)

    @property
    def is_expired(self) -> bool:
        if self.expires is None:
            return False
        return datetime.date.fromisoformat(self.expires) < datetime.date.today()


# =============================================================================
# Custom Exceptions (inherit ValueError for backward compat with gRPC handlers)
# =============================================================================


class TokenNotFoundError(ValueError):
    """Token not found in store."""

    def __init__(self):
        super().__init__("Invalid authentication token")


class TokenExpiredError(ValueError):
    """Token has expired."""

    def __init__(self, username: str):
        self.username = username
        super().__init__(f"Token expired for user '{username}'")


class RateLimitError(ValueError):
    """Rate limit exceeded."""

    def __init__(self, retry_after: int):
        self.retry_after = retry_after
        super().__init__(f"Rate limit exceeded. Retry after {retry_after}s")


class TopKExceededError(ValueError):
    """Requested top_k exceeds role limit."""

    def __init__(self, requested: int, max_top_k: int, role_name: str):
        self.requested = requested
        self.max_top_k = max_top_k
        super().__init__(f"top_k {requested} exceeds limit {max_top_k} for role '{role_name}'")


class ScopeError(ValueError):
    """Method not permitted for role."""

    def __init__(self, method: str, role_name: str):
        self.method = method
        super().__init__(f"Method '{method}' not permitted for role '{role_name}'")


# =============================================================================
# Rate Limiter (moved from vault_core.py)
# =============================================================================


class RateLimiter:
    """Simple sliding window rate limiter."""

    def __init__(self, max_requests: int = 30, window_seconds: int = 60):
        self.max_requests = max_requests
        self.window_seconds = window_seconds
        self._requests: dict[str, list[float]] = defaultdict(list)
        self._lock = threading.Lock()

    def is_allowed(self, client_id: str) -> bool:
        """Check if request is allowed and record it."""
        now = time.time()
        with self._lock:
            self._requests[client_id] = [
                t for t in self._requests[client_id] if now - t < self.window_seconds
            ]
            if len(self._requests[client_id]) >= self.max_requests:
                return False
            self._requests[client_id].append(now)
            return True

    def get_retry_after(self, client_id: str) -> int:
        """Returns seconds until next request is allowed."""
        with self._lock:
            if not self._requests[client_id]:
                return 0
            oldest = min(self._requests[client_id])
            return max(0, int(self.window_seconds - (time.time() - oldest)))

    def remove(self, client_id: str):
        """Remove a client's rate limit tracking."""
        with self._lock:
            self._requests.pop(client_id, None)


# =============================================================================
# Default Roles
# =============================================================================

DEFAULT_ROLES = {
    "admin": Role(
        "admin",
        ["get_public_key", "decrypt_scores", "decrypt_metadata", "manage_tokens"],
        50,
        "150/60s",
    ),
    "member": Role(
        "member",
        ["get_public_key", "decrypt_scores", "decrypt_metadata"],
        10,
        "30/60s",
    ),
}

DEFAULT_ROLE_NAMES = frozenset(DEFAULT_ROLES.keys())

DEMO_TOKEN = "evt_0000000000000000000000000000demo"


# =============================================================================
# Token Store
# =============================================================================


class TokenStore:
    """Thread-safe in-memory store for tokens and roles with async YAML persistence."""

    def __init__(self):
        self._lock = threading.RLock()
        self._tokens: dict[str, Token] = {}  # keyed by token string
        self._tokens_by_user: dict[str, Token] = {}  # keyed by username
        self._roles: dict[str, Role] = {}
        self._rate_limiters: dict[str, RateLimiter] = {}  # keyed by username
        self._roles_path: str | None = None
        self._tokens_path: str | None = None
        self._persist_executor = ThreadPoolExecutor(max_workers=1)

    # ── Loaders ──────────────────────────────────────────────────────────

    def load_from_files(self, roles_path: str, tokens_path: str):
        """Load roles and tokens from YAML config files at startup."""
        with self._lock:
            self._roles_path = roles_path
            self._tokens_path = tokens_path

            # Load roles
            if os.path.exists(roles_path):
                with open(roles_path) as f:
                    data = yaml.safe_load(f) or {}
                for name, cfg in data.get("roles", {}).items():
                    self._roles[name] = Role(
                        name=name,
                        scope=cfg.get("scope", []),
                        top_k=cfg.get("top_k", 5),
                        rate_limit=cfg.get("rate_limit", "30/60s"),
                    )
                logger.info("Loaded %d roles from %s", len(self._roles), roles_path)
            else:
                self._roles = dict(DEFAULT_ROLES)
                logger.info("No roles file found, using defaults")

            # Ensure default roles always exist
            for name, role in DEFAULT_ROLES.items():
                if name not in self._roles:
                    self._roles[name] = role

            # Load tokens
            if os.path.exists(tokens_path):
                with open(tokens_path) as f:
                    data = yaml.safe_load(f) or {}
                for entry in data.get("tokens", []):
                    tok = Token(
                        user=entry["user"],
                        token=entry["token"],
                        role=entry["role"],
                        issued_at=entry.get("issued_at") or entry.get("created", ""),
                        expires=entry.get("expires"),
                    )
                    self._tokens[tok.token] = tok
                    self._tokens_by_user[tok.user] = tok
                logger.info("Loaded %d tokens from %s", len(self._tokens), tokens_path)

        # Auto-generate default config files if they don't exist
        if not os.path.exists(roles_path) or not os.path.exists(tokens_path):
            self._schedule_persist()

    def load_legacy_env(self, env_tokens: str):
        """Backward compat: load comma-separated VAULT_TOKENS as legacy tokens."""
        with self._lock:
            self._roles = dict(DEFAULT_ROLES)
            tokens = [t.strip() for t in env_tokens.split(",") if t.strip()]
            for i, token_str in enumerate(tokens):
                user = f"legacy_{i}"
                tok = Token(
                    user=user,
                    token=token_str,
                    role="admin",
                    issued_at=datetime.date.today().isoformat(),
                    expires=None,
                )
                self._tokens[tok.token] = tok
                self._tokens_by_user[tok.user] = tok
            logger.info("Loaded %d legacy tokens from env var", len(tokens))

    def load_defaults_with_demo_token(self):
        """Demo mode: load default roles and demo token."""
        with self._lock:
            self._roles = dict(DEFAULT_ROLES)
            tok = Token(
                user="demo",
                token=DEMO_TOKEN,
                role="admin",
                issued_at=datetime.date.today().isoformat(),
                expires=None,
            )
            self._tokens[tok.token] = tok
            self._tokens_by_user[tok.user] = tok
            logger.warning("Demo mode active with demo token")

    # ── Validation ───────────────────────────────────────────────────────

    def validate(self, token_str: str) -> tuple[str, Role]:
        """
        Validate a token string.

        Returns (username, Role) on success.
        Raises TokenNotFoundError, TokenExpiredError, or RateLimitError on failure.
        """
        with self._lock:
            tok = self._tokens.get(token_str)
            if tok is None:
                raise TokenNotFoundError()

            if tok.is_expired:
                raise TokenExpiredError(tok.user)

            role = self._roles.get(tok.role)
            if role is None:
                raise TokenNotFoundError()

            # Per-user rate limiting with role-specific limits
            limiter = self._get_or_create_limiter(tok.user, role)

        # Rate limit check outside the main lock (limiter has its own lock)
        if not limiter.is_allowed(tok.user):
            retry_after = limiter.get_retry_after(tok.user)
            raise RateLimitError(retry_after)

        return tok.user, role

    def get_username(self, token_str: str) -> str | None:
        """Look up username for a token without side effects."""
        with self._lock:
            tok = self._tokens.get(token_str)
            return tok.user if tok else None

    def check_scope(self, role: Role, method_name: str):
        """Check if a method is permitted for the given role."""
        if method_name not in role.scope:
            raise ScopeError(method_name, role.name)

    def _get_or_create_limiter(self, username: str, role: Role) -> RateLimiter:
        """Get or lazily create a rate limiter for a user with role-specific limits."""
        # Must be called under self._lock
        limiter = self._rate_limiters.get(username)
        if limiter is None:
            max_req, window = role.rate_limit_parsed
            limiter = RateLimiter(max_requests=max_req, window_seconds=window)
            self._rate_limiters[username] = limiter
        return limiter

    # ── Token CRUD ───────────────────────────────────────────────────────

    def add_token(self, user: str, role: str, expires_days: int | None = None) -> Token:
        """Issue a new token for a user."""
        with self._lock:
            if role not in self._roles:
                raise ValueError(f"Role '{role}' does not exist")
            if user in self._tokens_by_user:
                raise ValueError(f"Token already exists for user '{user}'")

            token_str = f"evt_{secrets.token_hex(16)}"
            today = datetime.date.today()
            expires = None
            if expires_days is not None:
                expires = (today + datetime.timedelta(days=expires_days)).isoformat()

            tok = Token(
                user=user,
                token=token_str,
                role=role,
                issued_at=today.isoformat(),
                expires=expires,
            )
            self._tokens[tok.token] = tok
            self._tokens_by_user[tok.user] = tok

        self._schedule_persist()
        return tok

    def revoke_token(self, user: str) -> bool:
        """Revoke a user's token. Returns True if token was found and revoked."""
        with self._lock:
            tok = self._tokens_by_user.pop(user, None)
            if tok is None:
                return False
            self._tokens.pop(tok.token, None)
            # Clean up rate limiter
            limiter = self._rate_limiters.pop(user, None)
            if limiter:
                limiter.remove(user)

        self._schedule_persist()
        return True

    def rotate_token(self, user: str) -> Token:
        """Atomically revoke old token and issue a new one for the same user/role."""
        with self._lock:
            old_tok = self._tokens_by_user.get(user)
            if old_tok is None:
                raise ValueError(f"No token found for user '{user}'")

            old_role = old_tok.role
            # Preserve original expiry duration
            expires_days = None
            if old_tok.expires:
                issued_date = datetime.date.fromisoformat(old_tok.issued_at)
                expires_date = datetime.date.fromisoformat(old_tok.expires)
                expires_days = (expires_date - issued_date).days

            # Revoke old (inline, within same lock)
            self._tokens.pop(old_tok.token, None)
            del self._tokens_by_user[user]
            limiter = self._rate_limiters.pop(user, None)
            if limiter:
                limiter.remove(user)

            # Issue new
            token_str = f"evt_{secrets.token_hex(16)}"
            today = datetime.date.today()
            expires = None
            if expires_days is not None:
                expires = (today + datetime.timedelta(days=expires_days)).isoformat()

            new_tok = Token(
                user=user,
                token=token_str,
                role=old_role,
                issued_at=today.isoformat(),
                expires=expires,
            )
            self._tokens[new_tok.token] = new_tok
            self._tokens_by_user[user] = new_tok

        self._schedule_persist()
        logger.info("Rotated token for user '%s'", user)
        return new_tok

    def rotate_all_tokens(self) -> list[Token]:
        """Rotate all tokens. Each rotation is individually atomic."""
        with self._lock:
            users = list(self._tokens_by_user.keys())
        results = []
        for user in users:
            results.append(self.rotate_token(user))
        return results

    def list_tokens(self) -> list[dict]:
        """List all tokens (token values truncated for security)."""
        with self._lock:
            result = []
            for tok in self._tokens_by_user.values():
                role = self._roles.get(tok.role)
                result.append(
                    {
                        "user": tok.user,
                        "role": tok.role,
                        "top_k": role.top_k if role else "?",
                        "rate_limit": role.rate_limit if role else "?",
                        "expires": tok.expires or "never",
                    }
                )
            return result

    # ── Role CRUD ────────────────────────────────────────────────────────

    @staticmethod
    def _validate_rate_limit(rate_limit: str):
        """Validate rate_limit format (e.g. '30/60s')."""
        import re

        if not re.fullmatch(r"\d+/\d+s", rate_limit):
            raise ValueError(
                f"Invalid rate_limit format '{rate_limit}'."
                " Expected '<max>/<window>s' (e.g. '30/60s')"
            )

    def add_role(self, name: str, scope: list[str], top_k: int, rate_limit: str) -> Role:
        """Create a new role."""
        self._validate_rate_limit(rate_limit)
        with self._lock:
            if name in self._roles:
                raise ValueError(f"Role '{name}' already exists")
            role = Role(name=name, scope=scope, top_k=top_k, rate_limit=rate_limit)
            self._roles[name] = role

        self._schedule_persist()
        return role

    def update_role(self, name: str, **kwargs) -> Role:
        """Update an existing role. Accepts scope, top_k, rate_limit kwargs."""
        if "rate_limit" in kwargs:
            self._validate_rate_limit(kwargs["rate_limit"])
        with self._lock:
            role = self._roles.get(name)
            if role is None:
                raise ValueError(f"Role '{name}' does not exist")

            if "scope" in kwargs:
                role.scope = kwargs["scope"]
            if "top_k" in kwargs:
                role.top_k = kwargs["top_k"]
            if "rate_limit" in kwargs:
                role.rate_limit = kwargs["rate_limit"]
                # Clear rate limiters for affected users so they pick up new limits
                for tok in self._tokens_by_user.values():
                    if tok.role == name and tok.user in self._rate_limiters:
                        del self._rate_limiters[tok.user]

        self._schedule_persist()
        return role

    def delete_role(self, name: str):
        """Delete a role. Rejects deletion of default roles."""
        with self._lock:
            if name in DEFAULT_ROLE_NAMES:
                raise ValueError(f"Cannot delete default role '{name}'")
            if name not in self._roles:
                raise ValueError(f"Role '{name}' does not exist")

            # Check if any tokens reference this role
            for tok in self._tokens_by_user.values():
                if tok.role == name:
                    raise ValueError(
                        f"Cannot delete role '{name}': "
                        f"token for user '{tok.user}' is assigned to it"
                    )

            del self._roles[name]

        self._schedule_persist()

    def list_roles(self) -> list[dict]:
        """List all roles."""
        with self._lock:
            return [
                {
                    "name": r.name,
                    "scope": r.scope,
                    "top_k": r.top_k,
                    "rate_limit": r.rate_limit,
                }
                for r in self._roles.values()
            ]

    # ── Persistence ──────────────────────────────────────────────────────

    def _schedule_persist(self):
        """Schedule async persistence to YAML files."""
        if self._roles_path is None or self._tokens_path is None:
            return  # No file paths configured (legacy/demo mode)
        self._persist_executor.submit(self._do_persist)

    def _do_persist(self):
        """Atomically write current state to YAML files."""
        try:
            time.sleep(0.1)  # Debounce rapid changes

            with self._lock:
                roles_data = {
                    "roles": {
                        r.name: {
                            "scope": r.scope,
                            "top_k": r.top_k,
                            "rate_limit": r.rate_limit,
                        }
                        for r in self._roles.values()
                    }
                }
                tokens_data = {
                    "tokens": [
                        {
                            "user": t.user,
                            "token": t.token,
                            "role": t.role,
                            "issued_at": t.issued_at,
                            **({"expires": t.expires} if t.expires else {}),
                        }
                        for t in self._tokens_by_user.values()
                    ]
                }

            # Atomic writes: temp file + os.replace
            if self._roles_path:
                self._atomic_write(self._roles_path, roles_data)
            if self._tokens_path:
                self._atomic_write(self._tokens_path, tokens_data)

            logger.debug("Persisted token/role state to YAML")
        except Exception:
            logger.exception("Failed to persist token/role state")

    @staticmethod
    def _atomic_write(path: str, data: dict):
        """Write data to a file atomically via temp file + os.replace."""
        dir_name = os.path.dirname(path) or "."
        os.makedirs(dir_name, exist_ok=True)
        fd, tmp_path = tempfile.mkstemp(dir=dir_name, suffix=".tmp")
        try:
            with os.fdopen(fd, "w") as f:
                yaml.safe_dump(data, f, default_flow_style=False, sort_keys=False)
            os.replace(tmp_path, path)
        except Exception:
            os.unlink(tmp_path)
            raise


# Module-level singleton
token_store = TokenStore()
