#!/usr/bin/env bash
#
# Rune-Vault installer.
#
# Downloads, verifies, and installs the runevault daemon with systemd (Linux)
# or launchd (macOS) service registration.
#
# Usage:
#   sudo bash install.sh [options]
#
# Options:
#   --version <tag>      Install a specific release tag (default: latest)
#   --force              Overwrite existing config and TLS certificates
#   --non-interactive    Skip all prompts; supply secrets via env vars
#   --uninstall          Stop the service, remove files, optionally delete data
#
# Non-interactive env vars:
#   RUNEVAULT_TEAM_NAME              keys.index_name (required)
#   RUNEVAULT_ENVECTOR_ENDPOINT      envector.endpoint (required)
#   RUNEVAULT_ENVECTOR_API_KEY       envector.api_key
#   RUNEVAULT_ENVECTOR_API_KEY_FILE  envector.api_key_file (alternative)
#   RUNEVAULT_TEAM_SECRET            tokens.team_secret (auto-generated if unset)
#   RUNEVAULT_TLS_CERT_PATH          Path to existing TLS cert (skips auto-gen)
#   RUNEVAULT_TLS_KEY_PATH           Path to existing TLS key  (skips auto-gen)
#
# Dev/testing env vars (set by scripts/install-dev.sh):
#   RUNEVAULT_LOCAL_BINARY    Path to local binary; skips download + verification
#   RUNEVAULT_SKIP_VERIFY     Set to 1 to skip cosign verification
#   RUNEVAULT_INSTALL_PREFIX  Override /opt/runevault (default)
#   RUNEVAULT_BINARY_PATH     Override /usr/local/bin/runevault (default)
#   RUNEVAULT_SKIP_SERVICE    Set to 1 to skip systemd/launchd installation

set -euo pipefail

# ── Constants ──────────────────────────────────────────────────────────────────
REPO=CryptoLabInc/rune-admin
OIDC_ISSUER=https://token.actions.githubusercontent.com
CERT_REGEXP="^https://github.com/CryptoLabInc/rune-admin/.github/workflows/release.yaml@"
SERVICE_USER=runevault
GRPC_PORT=50051

# Overridable by env (used by scripts/install-dev.sh)
INSTALL_PREFIX="${RUNEVAULT_INSTALL_PREFIX:-/opt/runevault}"
BINARY_DEST="${RUNEVAULT_BINARY_PATH:-/usr/local/bin/runevault}"
SKIP_VERIFY="${RUNEVAULT_SKIP_VERIFY:-0}"
LOCAL_BINARY="${RUNEVAULT_LOCAL_BINARY:-}"
SKIP_SERVICE="${RUNEVAULT_SKIP_SERVICE:-0}"

# ── Color helpers ──────────────────────────────────────────────────────────────
if [[ -t 1 ]]; then
  _RED='\033[0;31m' _GRN='\033[0;32m' _BLU='\033[0;34m' _YLW='\033[0;33m' _RST='\033[0m'
else
  _RED='' _GRN='' _BLU='' _YLW='' _RST=''
fi
die()     { printf "${_RED}ERROR:${_RST} %s\n" "$*" >&2; exit 1; }
info()    { printf "${_BLU}==>${_RST} %s\n" "$*"; }
success() { printf "${_GRN}✓${_RST} %s\n" "$*"; }
warn()    { printf "${_YLW}WARNING:${_RST} %s\n" "$*" >&2; }

# ── Argument parsing ───────────────────────────────────────────────────────────
UNINSTALL=0
FORCE=0
VERSION=""
NON_INTERACTIVE=0

while [[ $# -gt 0 ]]; do
  case $1 in
    --version)         VERSION="$2"; shift 2 ;;
    --uninstall)       UNINSTALL=1; shift ;;
    --force)           FORCE=1; shift ;;
    --non-interactive) NON_INTERACTIVE=1; shift ;;
    *) die "Unknown flag: $1" ;;
  esac
done

# Auto-set non-interactive when stdin is not a TTY (e.g. curl | bash)
[[ -t 0 ]] || NON_INTERACTIVE=1

# ── Platform detection ─────────────────────────────────────────────────────────
case "$(uname -s)" in
  Linux)  OS_SLUG=linux ;;
  Darwin) OS_SLUG=darwin ;;
  *)      die "Unsupported OS: $(uname -s). Only Linux and macOS are supported." ;;
esac
case "$(uname -m)" in
  x86_64|amd64)  ARCH_SLUG=amd64 ;;
  arm64|aarch64) ARCH_SLUG=arm64 ;;
  *)             die "Unsupported architecture: $(uname -m)." ;;
esac

# ── Uninstall flow ─────────────────────────────────────────────────────────────
run_uninstall() {
  info "Uninstalling Rune-Vault..."
  [[ "$(id -u)" -eq 0 ]] || die "Uninstall must be run as root (use sudo)."

  if [[ "$OS_SLUG" = linux ]]; then
    if systemctl is-active --quiet runevault.service 2>/dev/null; then
      info "Stopping runevault.service..."
      systemctl stop runevault.service
    fi
    systemctl disable runevault.service 2>/dev/null || true
    rm -f /etc/systemd/system/runevault.service
    systemctl daemon-reload
    success "systemd service removed."
  else
    local plist=/Library/LaunchDaemons/com.cryptolabinc.runevault.plist
    if [[ -f "$plist" ]]; then
      launchctl bootout system/com.cryptolabinc.runevault 2>/dev/null || true
      rm -f "$plist"
      success "launchd service removed."
    fi
  fi

  rm -f "$BINARY_DEST"
  success "Binary removed: ${BINARY_DEST}"

  printf '\n'
  warn "The following directory contains Rune-Vault Keys and configuration:"
  warn "  ${INSTALL_PREFIX}/"
  warn "This data CANNOT be recovered if deleted."
  printf '\n'

  local answer=n
  if [[ "$NON_INTERACTIVE" -eq 1 ]]; then
    warn "Non-interactive mode: data preserved. Remove manually: rm -rf ${INSTALL_PREFIX}"
  else
    read -r -p "Delete all vault data including Rune-Vault Keys? [y/N] " answer
  fi

  case "$answer" in
    [Yy]*)
      rm -rf "${INSTALL_PREFIX}"
      success "Vault data deleted."
      ;;
    *)
      info "Data preserved at ${INSTALL_PREFIX}"
      ;;
  esac

  if [[ "$OS_SLUG" = linux ]]; then
    if id "$SERVICE_USER" >/dev/null 2>&1; then
      userdel "$SERVICE_USER" 2>/dev/null || true
      success "System user '${SERVICE_USER}' removed."
    fi
    if getent group "$SERVICE_USER" >/dev/null 2>&1; then
      groupdel "$SERVICE_USER" 2>/dev/null || true
      success "System group '${SERVICE_USER}' removed."
    fi
  else
    if id "$SERVICE_USER" >/dev/null 2>&1; then
      dscl . -delete /Users/"$SERVICE_USER" 2>/dev/null || true
      success "System user '${SERVICE_USER}' removed."
    fi
    if dscl . -read /Groups/"$SERVICE_USER" >/dev/null 2>&1; then
      dscl . -delete /Groups/"$SERVICE_USER" 2>/dev/null || true
      success "System group '${SERVICE_USER}' removed."
    fi
  fi

  success "Rune-Vault uninstalled."
}

# ── Tool auto-install ──────────────────────────────────────────────────────────

# Run brew as the original (non-root) user when invoked via sudo on macOS.
_brew() { sudo -u "${SUDO_USER:-$(id -un)}" brew "$@"; }

_pkg_install() {
  if command -v apt-get >/dev/null 2>&1; then
    apt-get install -y "$@"
  elif command -v dnf >/dev/null 2>&1; then
    dnf install -y "$@"
  elif command -v yum >/dev/null 2>&1; then
    yum install -y "$@"
  else
    die "No supported package manager found (apt/dnf/yum). Install manually: $*"
  fi
}

_install_tool() {
  local tool=$1
  info "Installing ${tool}..."
  case "$OS_SLUG:$tool" in
    linux:cosign)
      # Download pre-built binary from sigstore releases (no apt repo needed)
      local arch_suffix=amd64
      [[ "$ARCH_SLUG" = arm64 ]] && arch_suffix=arm64
      curl -fsSL \
        "https://github.com/sigstore/cosign/releases/latest/download/cosign-linux-${arch_suffix}" \
        -o /usr/local/bin/cosign
      chmod 0755 /usr/local/bin/cosign
      ;;
    linux:openssl)  _pkg_install openssl ;;
    linux:sha256sum) _pkg_install coreutils ;;
    darwin:cosign)  _brew install cosign ;;
    darwin:openssl) _brew install openssl ;;
    darwin:shasum)
      die "shasum is pre-installed on macOS. Something is very wrong." ;;
    *:systemctl)
      die "systemctl not found. This installer requires a systemd-based Linux." ;;
    *)
      die "Don't know how to install '${tool}' on ${OS_SLUG}. Install it manually." ;;
  esac
  command -v "$tool" >/dev/null 2>&1 \
    || die "Installation of '${tool}' appeared to succeed but binary not found in PATH."
  success "${tool} installed."
}

# ── Phase 1: Preflight ─────────────────────────────────────────────────────────
preflight() {
  info "Running preflight checks..."

  [[ "$(id -u)" -eq 0 ]] || die "This installer must be run as root (use sudo)."

  local tools=(curl)
  [[ "$SKIP_VERIFY" -eq 0 && -z "$LOCAL_BINARY" ]] && tools+=(cosign)
  if [[ "$OS_SLUG" = linux ]]; then
    tools+=(sha256sum systemctl)
  else
    tools+=(shasum)
  fi
  # openssl only needed when auto-generating TLS certs
  if [[ -z "${RUNEVAULT_TLS_CERT_PATH:-}" || -z "${RUNEVAULT_TLS_KEY_PATH:-}" ]]; then
    tools+=(openssl)
  fi

  # Collect missing tools (systemctl is never auto-installable — fail immediately)
  local missing=()
  for tool in "${tools[@]}"; do
    command -v "$tool" >/dev/null 2>&1 && continue
    [[ "$tool" = systemctl ]] \
      && die "systemctl not found. This installer requires a systemd-based Linux."
    missing+=("$tool")
  done

  if [[ ${#missing[@]} -gt 0 ]]; then
    printf '\n'
    warn "The following required tools are not installed:"
    for tool in "${missing[@]}"; do printf '  - %s\n' "$tool"; done
    printf '\n'

    local answer=n
    if [[ "$NON_INTERACTIVE" -eq 0 ]]; then
      read -r -p "Install missing tools automatically? [y/N] " answer
    else
      warn "Non-interactive mode: cannot auto-install missing tools."
    fi

    case "$answer" in
      [Yy]*)
        for tool in "${missing[@]}"; do
          _install_tool "$tool"
        done
        ;;
      *)
        printf 'Install them manually and re-run the installer:\n' >&2
        for tool in "${missing[@]}"; do
          case "$OS_SLUG:$tool" in
            linux:cosign)   printf '  cosign:    https://docs.sigstore.dev/cosign/system_config/installation/\n' >&2 ;;
            linux:openssl)  printf '  openssl:   apt install openssl\n' >&2 ;;
            linux:sha256sum) printf '  sha256sum: apt install coreutils\n' >&2 ;;
            darwin:cosign)  printf '  cosign:    brew install cosign\n' >&2 ;;
            darwin:openssl) printf '  openssl:   brew install openssl\n' >&2 ;;
          esac
        done
        exit 1
        ;;
    esac
  fi

  # Port availability (best-effort — skip gracefully if tools unavailable)
  local port_occupied=0
  if [[ "$OS_SLUG" = linux ]] && command -v ss >/dev/null 2>&1; then
    ss -tlnp 2>/dev/null | grep -q ":${GRPC_PORT}" && port_occupied=1 || true
  elif command -v lsof >/dev/null 2>&1; then
    lsof -iTCP:"${GRPC_PORT}" -sTCP:LISTEN -P -n 2>/dev/null \
      | grep -q ":${GRPC_PORT}" && port_occupied=1 || true
  fi
  if [[ "$port_occupied" -eq 1 ]]; then
    if [[ "$OS_SLUG" = linux ]]; then
      die "Port ${GRPC_PORT} is already in use. Stop the existing daemon first:
       sudo systemctl stop runevault"
    else
      die "Port ${GRPC_PORT} is already in use. Stop the existing daemon first:
       sudo launchctl bootout system/com.cryptolabinc.runevault"
    fi
  fi

  # Version resolution (skip if using a local binary)
  if [[ -z "$LOCAL_BINARY" && -z "$VERSION" ]]; then
    info "Resolving latest release version..."
    VERSION=$(curl -fsSL \
      "https://api.github.com/repos/${REPO}/releases/latest" \
      | grep '"tag_name"' \
      | head -1 \
      | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')
    [[ -n "$VERSION" ]] || die "Failed to resolve latest version from GitHub API."
    info "Latest version: ${VERSION}"
  fi

  # Already-installed version check (skip if --force or using a local binary)
  if [[ "$FORCE" -eq 0 && -z "$LOCAL_BINARY" && -x "$BINARY_DEST" ]]; then
    local installed_ver
    installed_ver=$("$BINARY_DEST" version 2>/dev/null | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+[^ ]*' | head -1 || true)
    if [[ -n "$installed_ver" && "$installed_ver" = "$VERSION" ]]; then
      warn "runevault ${VERSION} is already installed. Use --force to reinstall."
      exit 0
    fi
  fi

  success "Preflight checks passed."
}

# ── Phase 2 & 3: Download and verify ──────────────────────────────────────────
SCRATCH=""

_curl_retry() {
  local url=$1 dest=$2 i
  for i in 1 2 3; do
    curl -fsSL --connect-timeout 15 -o "$dest" "$url" && return 0
    warn "Download attempt ${i} failed for $(basename "$url"). Retrying in 5s..."
    sleep 5
  done
  die "Failed to download: ${url}"
}

_checksum_verify() {
  local sums_file=$1 archive=$2 archive_name line
  archive_name=$(basename "$archive")
  line=$(grep -F "$archive_name" "$sums_file") \
    || die "Archive '${archive_name}' not found in SHA256SUMS."
  (
    cd "$(dirname "$archive")"
    if [[ "$OS_SLUG" = linux ]]; then
      printf '%s\n' "$line" | sha256sum --check --quiet
    else
      printf '%s\n' "$line" | shasum -a 256 --check --quiet
    fi
  ) || die "Checksum verification failed for ${archive_name}."
}

download_and_verify() {
  SCRATCH=$(mktemp -d)
  trap 'rm -rf "$SCRATCH"' EXIT

  if [[ -n "$LOCAL_BINARY" ]]; then
    info "Using local binary: ${LOCAL_BINARY}"
    [[ -x "$LOCAL_BINARY" ]] || die "Local binary not executable: ${LOCAL_BINARY}"
    cp "$LOCAL_BINARY" "$SCRATCH/runevault"
    return 0
  fi

  local archive="runevault_${VERSION}_${OS_SLUG}_${ARCH_SLUG}.tar.gz"
  local base_url="https://github.com/${REPO}/releases/download/${VERSION}"

  info "Downloading ${archive}..."
  _curl_retry "${base_url}/${archive}"         "$SCRATCH/${archive}"
  _curl_retry "${base_url}/SHA256SUMS"         "$SCRATCH/SHA256SUMS"
  _curl_retry "${base_url}/SHA256SUMS.sig"     "$SCRATCH/SHA256SUMS.sig"
  _curl_retry "${base_url}/SHA256SUMS.pem"     "$SCRATCH/SHA256SUMS.pem"

  if [[ "$SKIP_VERIFY" -eq 1 ]]; then
    warn "SKIP_VERIFY=1: skipping cosign verification (development only)."
  else
    info "Verifying release signature..."
    local cosign_err
    cosign_err=$(mktemp)
    cosign verify-blob \
      --signature   "$SCRATCH/SHA256SUMS.sig" \
      --certificate "$SCRATCH/SHA256SUMS.pem" \
      --certificate-oidc-issuer "$OIDC_ISSUER" \
      --certificate-identity-regexp "$CERT_REGEXP" \
      "$SCRATCH/SHA256SUMS" 2>"$cosign_err" \
      || { cat "$cosign_err" >&2
           die "Signature verification failed — aborting before any installation."; }
    success "Signature verified."
  fi

  info "Verifying checksum..."
  _checksum_verify "$SCRATCH/SHA256SUMS" "$SCRATCH/${archive}"
  success "Checksum verified."

  info "Extracting binary..."
  tar -xzf "$SCRATCH/${archive}" -C "$SCRATCH" ./runevault
  "$SCRATCH/runevault" version >/dev/null 2>&1 \
    || die "Extracted binary failed smoke test."
}

# ── Phase 4: System setup ──────────────────────────────────────────────────────
_create_system_group() {
  if [[ "$OS_SLUG" = linux ]]; then
    if ! getent group "$SERVICE_USER" >/dev/null 2>&1; then
      groupadd --system "$SERVICE_USER"
      success "System group '${SERVICE_USER}' created."
    else
      info "System group '${SERVICE_USER}' already exists."
    fi
  else
    if ! dscl . -read /Groups/"$SERVICE_USER" >/dev/null 2>&1; then
      local gid=490
      while dscl . -list /Groups PrimaryGroupID 2>/dev/null \
            | awk '{print $2}' | grep -qx "$gid"; do
        gid=$((gid - 1))
      done
      dscl . -create /Groups/"$SERVICE_USER"
      dscl . -create /Groups/"$SERVICE_USER" PrimaryGroupID "$gid"
      dscl . -create /Groups/"$SERVICE_USER" RealName "Rune Vault Admin Group"
      success "System group '${SERVICE_USER}' created (GID=${gid})."
    else
      info "System group '${SERVICE_USER}' already exists."
    fi
  fi
}

_create_system_user() {
  if [[ "$OS_SLUG" = linux ]]; then
    if ! id "$SERVICE_USER" >/dev/null 2>&1; then
      useradd --system --no-create-home --shell /usr/sbin/nologin \
        -g "$SERVICE_USER" --no-user-group "$SERVICE_USER"
      success "System user '${SERVICE_USER}' created."
    else
      info "System user '${SERVICE_USER}' already exists."
    fi
  else
    if ! id "$SERVICE_USER" >/dev/null 2>&1; then
      local uid=490
      while dscl . -list /Users UniqueID 2>/dev/null \
            | awk '{print $2}' | grep -qx "$uid"; do
        uid=$((uid - 1))
      done
      local gid
      gid=$(dscl . -read /Groups/"$SERVICE_USER" PrimaryGroupID 2>/dev/null \
            | awk '{print $2}')
      dscl . -create /Users/"$SERVICE_USER"
      dscl . -create /Users/"$SERVICE_USER" UserShell        /usr/bin/false
      dscl . -create /Users/"$SERVICE_USER" RealName         "Rune Vault Service"
      dscl . -create /Users/"$SERVICE_USER" UniqueID         "$uid"
      dscl . -create /Users/"$SERVICE_USER" PrimaryGroupID   "$gid"
      dscl . -create /Users/"$SERVICE_USER" NFSHomeDirectory /var/empty
      dscl . -create /Users/"$SERVICE_USER" IsHidden         1
      success "System user '${SERVICE_USER}' created (UID=${uid})."
    else
      info "System user '${SERVICE_USER}' already exists."
    fi
  fi
}

_add_invoking_user_to_group() {
  local invoking_user="${SUDO_USER:-}"
  [[ -z "$invoking_user" ]] && return 0
  if [[ "$OS_SLUG" = linux ]]; then
    usermod -aG "$SERVICE_USER" "$invoking_user"
  else
    dscl . -append /Groups/"$SERVICE_USER" GroupMembership "$invoking_user" 2>/dev/null || true
  fi
  success "Added '${invoking_user}' to group '${SERVICE_USER}'."
}

setup_system() {
  info "Setting up system..."

  if [[ "$SKIP_SERVICE" -eq 0 ]]; then
    _create_system_group
    _create_system_user
  fi

  # /opt may not exist on fresh macOS
  [[ "$OS_SLUG" = darwin ]] && mkdir -p /opt

  local dir
  for dir in \
    "${INSTALL_PREFIX}" \
    "${INSTALL_PREFIX}/configs" \
    "${INSTALL_PREFIX}/certs" \
    "${INSTALL_PREFIX}/logs"
  do
    mkdir -p "$dir"
    chmod 0750 "$dir"
    [[ "$SKIP_SERVICE" -eq 0 ]] && chown "${SERVICE_USER}:${SERVICE_USER}" "$dir"
  done
  # vault-keys stays 0700: secret FHE key material must never be group-readable.
  mkdir -p "${INSTALL_PREFIX}/vault-keys"
  chmod 0700 "${INSTALL_PREFIX}/vault-keys"
  [[ "$SKIP_SERVICE" -eq 0 ]] && chown "${SERVICE_USER}:${SERVICE_USER}" "${INSTALL_PREFIX}/vault-keys"

  success "Directories created under ${INSTALL_PREFIX}/"

  install -m 0755 "$SCRATCH/runevault" "$BINARY_DEST"
  success "Binary installed: ${BINARY_DEST}"

  [[ "$SKIP_SERVICE" -eq 0 ]] && _add_invoking_user_to_group
}

# ── Phase 5: TLS certificates ──────────────────────────────────────────────────
generate_tls_certs() {
  local cert_dir="${INSTALL_PREFIX}/certs"

  # BYO cert: copy provided files and skip generation
  if [[ -n "${RUNEVAULT_TLS_CERT_PATH:-}" && -n "${RUNEVAULT_TLS_KEY_PATH:-}" ]]; then
    cp "${RUNEVAULT_TLS_CERT_PATH}" "${cert_dir}/server.pem"
    cp "${RUNEVAULT_TLS_KEY_PATH}"  "${cert_dir}/server.key"
    chmod 0644 "${cert_dir}/server.pem"
    chmod 0600 "${cert_dir}/server.key"
    [[ "$SKIP_SERVICE" -eq 0 ]] \
      && chown "$SERVICE_USER" "${cert_dir}/server.pem" "${cert_dir}/server.key"
    info "Using provided TLS certificates."
    return 0
  fi

  if [[ -f "${cert_dir}/server.pem" && "$FORCE" -eq 0 ]]; then
    info "TLS certificates already exist (use --force to regenerate)."
    return 0
  fi

  info "Generating self-signed TLS certificates..."

  local public_ip=""
  public_ip=$(curl -4 -sf --connect-timeout 5 ifconfig.me 2>/dev/null || true)
  [[ -n "$public_ip" ]] && info "Public IP detected: ${public_ip}"

  # Write openssl config via printf (avoids heredoc issues in piped execution)
  local tmpconf
  tmpconf=$(mktemp)
  printf '[req]\ndistinguished_name = req_dn\nreq_extensions = v3_req\nprompt = no\n\n' \
    > "$tmpconf"
  printf '[req_dn]\nCN = runevault\n\n'              >> "$tmpconf"
  printf '[v3_req]\nsubjectAltName = @alt_names\n\n' >> "$tmpconf"
  printf '[alt_names]\n'                             >> "$tmpconf"
  printf 'DNS.1 = localhost\n'                       >> "$tmpconf"
  printf 'DNS.2 = vault\n'                           >> "$tmpconf"
  printf 'DNS.3 = runevault\n'                       >> "$tmpconf"
  printf 'IP.1  = 127.0.0.1\n'                       >> "$tmpconf"
  [[ -n "$public_ip" ]] && printf 'IP.2  = %s\n' "$public_ip" >> "$tmpconf"

  openssl genrsa -out "${cert_dir}/ca.key" 4096 2>/dev/null
  openssl req -new -x509 \
    -key "${cert_dir}/ca.key" \
    -out "${cert_dir}/ca.pem" \
    -days 3650 -subj "/CN=Rune-Vault CA" -sha256 2>/dev/null

  openssl genrsa -out "${cert_dir}/server.key" 2048 2>/dev/null
  local csr="${cert_dir}/server.csr"
  openssl req -new \
    -key "${cert_dir}/server.key" -out "$csr" -config "$tmpconf" 2>/dev/null
  openssl x509 -req \
    -in "$csr" \
    -CA "${cert_dir}/ca.pem" -CAkey "${cert_dir}/ca.key" -CAcreateserial \
    -out "${cert_dir}/server.pem" \
    -days 825 -sha256 -extfile "$tmpconf" -extensions v3_req 2>/dev/null

  rm -f "$tmpconf" "$csr" "${cert_dir}/ca.srl"

  chmod 0600 "${cert_dir}/ca.key" "${cert_dir}/server.key"
  chmod 0644 "${cert_dir}/ca.pem" "${cert_dir}/server.pem"
  if [[ "$SKIP_SERVICE" -eq 0 ]]; then
    chown "${SERVICE_USER}:${SERVICE_USER}" \
      "${cert_dir}/ca.key" "${cert_dir}/ca.pem" \
      "${cert_dir}/server.key" "${cert_dir}/server.pem"
  fi

  success "TLS certificates generated."
}

# ── Phase 6: Configuration ─────────────────────────────────────────────────────
collect_and_write_config() {
  local conf_file="${INSTALL_PREFIX}/configs/runevault.conf"

  if [[ -f "$conf_file" && "$FORCE" -eq 0 ]]; then
    info "Config already exists (use --force to overwrite): ${conf_file}"
  else
    local team_name="${RUNEVAULT_TEAM_NAME:-}"
    local envector_endpoint="${RUNEVAULT_ENVECTOR_ENDPOINT:-}"
    local envector_api_key="${RUNEVAULT_ENVECTOR_API_KEY:-}"
    local envector_api_key_file="${RUNEVAULT_ENVECTOR_API_KEY_FILE:-}"
    local team_secret="${RUNEVAULT_TEAM_SECRET:-}"

    if [[ "$NON_INTERACTIVE" -eq 0 ]]; then
      printf '\n'
      printf '══════════════════════════════════════════════════════════\n'
      printf '  Vault configuration\n'
      printf '══════════════════════════════════════════════════════════\n'
      printf '\n'
      [[ -z "$team_name" ]] \
        && read -r -p "Team name (vault index identifier): " team_name
      [[ -z "$envector_endpoint" ]] \
        && read -r -p "enVector endpoint URL: " envector_endpoint
      if [[ -z "$envector_api_key" && -z "$envector_api_key_file" ]]; then
        read -r -p "enVector API key: " envector_api_key
      fi
      printf '\n'
    else
      local missing=()
      [[ -z "$team_name" ]]         && missing+=("RUNEVAULT_TEAM_NAME")
      [[ -z "$envector_endpoint" ]] && missing+=("RUNEVAULT_ENVECTOR_ENDPOINT")
      [[ -z "$envector_api_key" && -z "$envector_api_key_file" ]] \
        && missing+=("RUNEVAULT_ENVECTOR_API_KEY or RUNEVAULT_ENVECTOR_API_KEY_FILE")
      if [[ ${#missing[@]} -gt 0 ]]; then
        printf 'ERROR: Missing required env vars for non-interactive install:\n' >&2
        for v in "${missing[@]}"; do printf '  %s\n' "$v" >&2; done
        exit 1
      fi
    fi

    if [[ -z "$team_secret" ]]; then
      team_secret=$(LC_ALL=C tr -dc 'a-f0-9' < /dev/urandom | head -c 64; true)
    fi

    [[ -n "$team_name" ]]         || die "team_name is required."
    [[ -n "$envector_endpoint" ]] || die "envector_endpoint is required."
    [[ -n "$envector_api_key" || -n "$envector_api_key_file" ]] \
      || die "enVector API key or key file is required."

    local api_key_line
    if [[ -n "$envector_api_key_file" ]]; then
      api_key_line="  api_key_file: ${envector_api_key_file}"
    else
      api_key_line="  api_key: ${envector_api_key}"
    fi

    info "Writing ${conf_file}..."
    printf '%s\n' \
      "server:" \
      "  grpc:" \
      "    host: 0.0.0.0" \
      "    port: ${GRPC_PORT}" \
      "    tls:" \
      "      cert: ${INSTALL_PREFIX}/certs/server.pem" \
      "      key: ${INSTALL_PREFIX}/certs/server.key" \
      "      disable: false" \
      "  admin:" \
      "    socket: ${INSTALL_PREFIX}/admin.sock" \
      "" \
      "keys:" \
      "  path: ${INSTALL_PREFIX}/vault-keys" \
      "  index_name: ${team_name}" \
      "  embedding_dim: 1024" \
      "" \
      "envector:" \
      "  endpoint: ${envector_endpoint}" \
      "${api_key_line}" \
      "" \
      "tokens:" \
      "  team_secret: ${team_secret}" \
      "  roles_file: ${INSTALL_PREFIX}/configs/roles.yml" \
      "  tokens_file: ${INSTALL_PREFIX}/configs/tokens.yml" \
      "" \
      "audit:" \
      "  mode: file+stdout" \
      "  path: ${INSTALL_PREFIX}/logs/audit.log" \
      > "$conf_file"
    chmod 0640 "$conf_file"
    [[ "$SKIP_SERVICE" -eq 0 ]] && chown "${SERVICE_USER}:${SERVICE_USER}" "$conf_file"

  fi

  # roles.yml
  local roles_file="${INSTALL_PREFIX}/configs/roles.yml"
  if [[ ! -f "$roles_file" || "$FORCE" -eq 1 ]]; then
    printf '%s\n' \
      "roles:" \
      "  admin:" \
      "    scope:" \
      "      - get_public_key" \
      "      - decrypt_scores" \
      "      - decrypt_metadata" \
      "      - manage_tokens" \
      "    top_k: 50" \
      "    rate_limit: 150/60s" \
      "  member:" \
      "    scope:" \
      "      - get_public_key" \
      "      - decrypt_scores" \
      "      - decrypt_metadata" \
      "    top_k: 10" \
      "    rate_limit: 30/60s" \
      > "$roles_file"
    chmod 0640 "$roles_file"
    [[ "$SKIP_SERVICE" -eq 0 ]] && chown "${SERVICE_USER}:${SERVICE_USER}" "$roles_file"
  fi

  # tokens.yml
  local tokens_file="${INSTALL_PREFIX}/configs/tokens.yml"
  if [[ ! -f "$tokens_file" || "$FORCE" -eq 1 ]]; then
    printf 'tokens: []\n' > "$tokens_file"
    chmod 0640 "$tokens_file"
    [[ "$SKIP_SERVICE" -eq 0 ]] && chown "${SERVICE_USER}:${SERVICE_USER}" "$tokens_file"
  fi

  success "Configuration written."
}

# ── Phase 7: Service installation ─────────────────────────────────────────────
install_service() {
  if [[ "$SKIP_SERVICE" -eq 1 ]]; then
    info "Skipping service installation (RUNEVAULT_SKIP_SERVICE=1)."
    return 0
  fi

  local config_path="${INSTALL_PREFIX}/configs/runevault.conf"

  if [[ "$OS_SLUG" = linux ]]; then
    if systemctl is-active --quiet runevault.service 2>/dev/null; then
      info "Stopping running runevault service..."
      systemctl stop runevault.service
      info "Tip: manage the service with: sudo systemctl start|stop|restart runevault"
    fi
    info "Installing systemd service..."
    local unit=/etc/systemd/system/runevault.service
    printf '%s\n' \
      "[Unit]" \
      "Description=Rune-Vault FHE gRPC Server" \
      "Documentation=https://github.com/${REPO}" \
      "After=network-online.target" \
      "Wants=network-online.target" \
      "" \
      "[Service]" \
      "Type=simple" \
      "User=${SERVICE_USER}" \
      "Group=${SERVICE_USER}" \
      "ExecStart=${BINARY_DEST} daemon start --config ${config_path}" \
      "Restart=on-failure" \
      "RestartSec=5s" \
      "TimeoutStopSec=30s" \
      "StandardOutput=journal" \
      "StandardError=journal" \
      "SyslogIdentifier=runevault" \
      "NoNewPrivileges=true" \
      "PrivateTmp=true" \
      "ProtectSystem=strict" \
      "ProtectHome=true" \
      "ReadWritePaths=${INSTALL_PREFIX}" \
      "ProtectKernelTunables=true" \
      "ProtectKernelModules=true" \
      "ProtectControlGroups=true" \
      "RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX" \
      "RestrictNamespaces=true" \
      "LockPersonality=true" \
      "MemoryDenyWriteExecute=false" \
      "RestrictRealtime=true" \
      "RestrictSUIDSGID=true" \
      "RemoveIPC=true" \
      "LimitNOFILE=65536" \
      "" \
      "[Install]" \
      "WantedBy=multi-user.target" \
      > "$unit"
    chmod 0644 "$unit"
    systemctl daemon-reload
    systemctl enable runevault.service
    systemctl start runevault.service
    success "systemd service enabled and started."

  else
    info "Installing launchd service..."
    local plist=/Library/LaunchDaemons/com.cryptolabinc.runevault.plist
    printf '%s\n' \
      '<?xml version="1.0" encoding="UTF-8"?>' \
      '<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"' \
      '  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">' \
      '<plist version="1.0">' \
      '<dict>' \
      '  <key>Label</key>' \
      '  <string>com.cryptolabinc.runevault</string>' \
      '' \
      '  <key>ProgramArguments</key>' \
      '  <array>' \
      "    <string>${BINARY_DEST}</string>" \
      '    <string>daemon</string>' \
      '    <string>start</string>' \
      '    <string>--config</string>' \
      "    <string>${config_path}</string>" \
      '  </array>' \
      '' \
      '  <key>UserName</key>' \
      "  <string>${SERVICE_USER}</string>" \
      '' \
      '  <key>RunAtLoad</key>' \
      '  <true/>' \
      '' \
      '  <key>KeepAlive</key>' \
      '  <true/>' \
      '' \
      '  <key>ThrottleInterval</key>' \
      '  <integer>10</integer>' \
      '' \
      '  <key>StandardOutPath</key>' \
      "  <string>${INSTALL_PREFIX}/logs/runevault.stdout.log</string>" \
      '' \
      '  <key>StandardErrorPath</key>' \
      "  <string>${INSTALL_PREFIX}/logs/runevault.stderr.log</string>" \
      '' \
      '  <key>EnvironmentVariables</key>' \
      '  <dict>' \
      '    <key>PATH</key>' \
      '    <string>/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin</string>' \
      '  </dict>' \
      '' \
      '  <key>ProcessType</key>' \
      '  <string>Background</string>' \
      '</dict>' \
      '</plist>' \
      > "$plist"
    chmod 0644 "$plist"
    chown root "$plist"
    launchctl bootout system/com.cryptolabinc.runevault 2>/dev/null || true
    launchctl bootstrap system "$plist"
    success "launchd service loaded."
  fi
}

# ── Phase 8: Post-install summary ─────────────────────────────────────────────
post_install() {
  if [[ "$SKIP_SERVICE" -eq 0 ]]; then
    info "Waiting for vault to start..."
    local i
    for i in $(seq 1 15); do
      "$BINARY_DEST" status \
        --config "${INSTALL_PREFIX}/configs/runevault.conf" \
        >/dev/null 2>&1 && { success "Vault is up."; break; } || true
      sleep 1
    done
  fi

  local public_ip=""
  public_ip=$(curl -4 -sf --connect-timeout 5 ifconfig.me 2>/dev/null || true)

  printf '\n'
  success "Rune-Vault ${VERSION:-local} installed successfully."
  printf '\n'
  printf '  Binary:   %s\n' "$BINARY_DEST"
  printf '  Config:   %s\n' "${INSTALL_PREFIX}/configs/runevault.conf"
  printf '  CA cert:  %s\n' "${INSTALL_PREFIX}/certs/ca.pem"
  [[ -n "$public_ip" ]] && printf '  Endpoint: %s:%s\n' "$public_ip" "$GRPC_PORT"
  printf '\n'
  printf 'Next steps:\n'
  printf '  Issue a token:  runevault token issue --user <name> --role member\n'
  printf '  Check status:   runevault status\n'
  if [[ "$OS_SLUG" = linux ]]; then
    printf '  View logs:      journalctl -u runevault -f\n'
    printf '  Manage daemon:  sudo systemctl start|stop|restart runevault\n'
  else
    printf '  View logs:      tail -f %s/logs/runevault.stderr.log\n' "${INSTALL_PREFIX}"
    printf '  Manage daemon:  sudo launchctl bootout system/com.cryptolabinc.runevault\n'
    printf '                  sudo launchctl bootstrap system /Library/LaunchDaemons/com.cryptolabinc.runevault.plist\n'
  fi
  if [[ -n "${SUDO_USER:-}" ]]; then
    printf '\n'
    printf "NOTE: '%s' was added to the '%s' group.\n" "${SUDO_USER}" "${SERVICE_USER}"
    printf '      Re-login (or run: newgrp %s) to apply group membership.\n' "${SERVICE_USER}"
  fi
  printf '\n'
  warn "BACKUP: Keep these safe — they cannot be recovered if lost:"
  warn "  Rune-Vault Keys: ${INSTALL_PREFIX}/vault-keys/"
  warn "  Config:          ${INSTALL_PREFIX}/configs/runevault.conf"
}

# ── Main ───────────────────────────────────────────────────────────────────────
if [[ "$UNINSTALL" -eq 1 ]]; then
  run_uninstall
  exit 0
fi

preflight
download_and_verify
setup_system
generate_tls_certs
collect_and_write_config
install_service
post_install
