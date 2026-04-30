#!/usr/bin/env bash
#
# Rune-Vault dev installer (sibling of install.sh).
#
# Installs the runevault daemon from your local working tree — never from a
# published release. Use this to verify in-progress source code on your local
# machine or on a CSP VM (AWS, GCP, OCI) before cutting a release.
#
# Usage:
#   sudo bash scripts/install-dev.sh [options]
#
# Options:
#   --target <local|aws|gcp|oci>  Install/uninstall target (default: prompt if TTY, else local)
#   --install-dir <path>          CSP install dir (default: $HOME/rune-vault-<csp>)
#   --prefix <dir>                Local-only: rootless test prefix
#   --non-interactive             Skip all prompts; supply secrets via env vars
#   --uninstall                   Forward uninstall to install.sh (local or CSP target)
#   --force                       Forwarded to install.sh (local target only)
#
# Differences from install.sh:
#   - Always installs from the local working tree (no GitHub release download).
#   - For CSP targets, builds linux/amd64 in Docker (golang:1.25-bookworm) with
#     --platform linux/amd64 — works on any host arch via qemu emulation.
#   - cloud-init-dev / startup-script-dev only prepare the VM; install.sh runs
#     over SSH after cloud-init finishes.
#
# Non-interactive env vars (CSP install — operator workstation):
#   RUNEVAULT_ENVECTOR_ENDPOINT      enVector endpoint URL (required)
#   RUNEVAULT_ENVECTOR_API_KEY       enVector API key (required)
#   RUNEVAULT_TEAM_NAME              Team name (required)
#   RUNEVAULT_TARGET                 Pre-select target without interactive menu
#   RUNEVAULT_INSTALL_DIR            Pre-set CSP install directory
#   RUNEVAULT_CSP_REGION             Cloud region
#   RUNEVAULT_GCP_PROJECT_ID         GCP: project ID (required for GCP)
#   RUNEVAULT_OCI_COMPARTMENT_ID     OCI: compartment OCID (required for OCI)

set -euo pipefail

# ── Constants ──────────────────────────────────────────────────────────────────
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
REPO_ROOT=$(cd "${SCRIPT_DIR}/.." && pwd)
LOCAL_BINARY_HOST="${REPO_ROOT}/vault/bin/runevault"
TARGET_OS=linux
TARGET_ARCH=amd64
LINUX_BINARY="${REPO_ROOT}/vault/bin/runevault-${TARGET_OS}-${TARGET_ARCH}"
BUILDER_IMAGE="golang:1.25-bookworm"
GRPC_PORT=50051

# Overridable by env (mirrors install.sh)
TARGET="${RUNEVAULT_TARGET:-}"
INSTALL_DIR_CSP="${RUNEVAULT_INSTALL_DIR:-}"
CSP_PUBLIC_IP=""

# CSP config (populated by dev_csp_prompt_config)
TEAM_NAME=""
ENVECTOR_ENDPOINT=""
ENVECTOR_API_KEY=""
CSP_REGION=""
GCP_PROJECT_ID=""
OCI_COMPARTMENT_ID=""

# ── Color helpers (copied from install.sh) ─────────────────────────────────────
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
PREFIX=""
NON_INTERACTIVE=0
UNINSTALL=0
PASSTHROUGH_ARGS=()

while [[ $# -gt 0 ]]; do
  case $1 in
    --target)          TARGET="$2"; shift 2 ;;
    --install-dir)     INSTALL_DIR_CSP="$2"; shift 2 ;;
    --prefix)          PREFIX="$2"; shift 2 ;;
    --non-interactive) NON_INTERACTIVE=1; PASSTHROUGH_ARGS+=("$1"); shift ;;
    --uninstall)       UNINSTALL=1; shift ;;
    --force)           PASSTHROUGH_ARGS+=("$1"); shift ;;
    *)                 PASSTHROUGH_ARGS+=("$1"); shift ;;
  esac
done

# Auto-set non-interactive when stdin is not a TTY
[[ -t 0 ]] || NON_INTERACTIVE=1

# ── Platform detection ─────────────────────────────────────────────────────────
case "$(uname -s)" in
  Linux)  HOST_OS=linux ;;
  Darwin) HOST_OS=darwin ;;
  *)      die "Unsupported host OS: $(uname -s). Only Linux and macOS are supported." ;;
esac
case "$(uname -m)" in
  x86_64|amd64)  HOST_ARCH=amd64 ;;
  arm64|aarch64) HOST_ARCH=arm64 ;;
  *)             die "Unsupported host architecture: $(uname -m)." ;;
esac

# ── Banner ─────────────────────────────────────────────────────────────────────
print_banner() {
  local commit
  commit=$(cd "$REPO_ROOT" && git rev-parse --short HEAD 2>/dev/null || echo unknown)
  printf '\n'
  printf '  ╭───────────────────────────────────────────────────────────────────╮\n'
  printf '  │  Rune-Vault dev installer                                         │\n'
  printf '  │  Source: local working tree (not a published release)             │\n'
  printf '  │  Commit: %-56s │\n' "$commit"
  printf '  ╰───────────────────────────────────────────────────────────────────╯\n'
  printf '\n'
}

# ── Helpers (mirror install.sh) ───────────────────────────────────────────────
_prompt() {
  local varname=$1 label=$2 default=${3:-}
  [[ -n "${!varname:-}" ]] && return 0
  local val
  if [[ -n "$default" ]]; then
    read -r -p "${label} [${default}]: " val
    printf -v "$varname" '%s' "${val:-$default}"
  else
    read -r -p "${label}: " val
    printf -v "$varname" '%s' "$val"
  fi
}

# Escape for embedding inside a double-quoted Terraform string.
escape_tf() { printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'; }

# Escape for embedding inside a single-quoted shell argument.
# Replaces every ' with '\''.
escape_single() {
  local s=$1
  printf '%s' "${s//\'/\'\\\'\'}"
}

# ── Target resolution (mirror install.sh:198–226) ─────────────────────────────
resolve_target() {
  if [[ -n "${TARGET:-}" ]]; then
    case "$TARGET" in
      local|aws|gcp|oci) ;;
      *) die "Invalid --target value: ${TARGET}. Valid: local, aws, gcp, oci." ;;
    esac
    return 0
  fi
  if [[ "$NON_INTERACTIVE" -eq 0 && -t 0 ]]; then
    local action="install"
    [[ "$UNINSTALL" -eq 1 ]] && action="uninstall"
    printf '  Select %s target:\n' "$action"
    printf '    1) Local (this machine)\n'
    printf '    2) AWS\n'
    printf '    3) GCP\n'
    printf '    4) OCI\n'
    printf '\n'
    local choice
    read -r -p "  Choice [1]: " choice
    case "${choice:-1}" in
      1|local) TARGET=local ;;
      2|aws)   TARGET=aws ;;
      3|gcp)   TARGET=gcp ;;
      4|oci)   TARGET=oci ;;
      *) die "Invalid choice: ${choice}" ;;
    esac
  else
    TARGET=local
  fi
}

# ── Preflight ──────────────────────────────────────────────────────────────────
dev_preflight() {
  info "Running dev preflight checks..."

  # Rootless local test (--prefix) is the one path that doesn't require sudo.
  if [[ "$TARGET" != "local" || -z "$PREFIX" ]]; then
    [[ "$(id -u)" -eq 0 ]] || die "This installer must be run as root (use sudo)."
  fi

  [[ -d "${REPO_ROOT}/vault" ]] \
    || die "vault/ directory not found under ${REPO_ROOT}. Run from a clone of rune-admin."

  local missing=()
  for tool in git mise; do
    command -v "$tool" >/dev/null 2>&1 || missing+=("$tool")
  done
  [[ ${#missing[@]} -gt 0 ]] && die "Missing required tools: ${missing[*]}"

  if [[ "$TARGET" != "local" ]]; then
    [[ -z "$PREFIX" ]] || die "--prefix is local-only."
    dev_check_docker
  fi

  success "Preflight passed."
}

dev_check_docker() {
  command -v docker >/dev/null 2>&1 \
    || die "docker is required for CSP targets. Install Docker Desktop / Docker Engine and retry."

  local docker_user="${SUDO_USER:-$(id -un)}"
  if ! sudo -u "$docker_user" -H bash -lc 'docker info' >/dev/null 2>&1; then
    die "docker daemon is not reachable for user '${docker_user}'. Start Docker (Docker Desktop / 'colima start' / 'systemctl start docker') and retry."
  fi

  # Cross-arch builder probe — fails fast if binfmt handlers are missing.
  if ! sudo -u "$docker_user" -H bash -lc \
    "docker run --rm --platform ${TARGET_OS}/${TARGET_ARCH} alpine:latest true" >/dev/null 2>&1; then
    die "docker cannot run ${TARGET_OS}/${TARGET_ARCH} images. Install qemu binfmt handlers:
       docker run --rm --privileged tonistiigi/binfmt --install all"
  fi
}

# ── Build ──────────────────────────────────────────────────────────────────────
dev_build_local_binary() {
  info "Building runevault for host (${HOST_OS}/${HOST_ARCH})..."
  local build_user="${SUDO_USER:-$(id -un)}"
  (cd "$REPO_ROOT" && sudo -u "$build_user" -H bash -lc 'mise run go:build')
  [[ -x "$LOCAL_BINARY_HOST" ]] || die "Build did not produce ${LOCAL_BINARY_HOST}."
  success "Built: ${LOCAL_BINARY_HOST}"
}

dev_build_linux_binary() {
  info "Building runevault for ${TARGET_OS}/${TARGET_ARCH} via Docker (${BUILDER_IMAGE})..."
  local build_user="${SUDO_USER:-$(id -un)}"
  local user_home commit version date pkg
  user_home="${SUDO_USER:+$(eval echo ~"${SUDO_USER}")}"
  user_home="${user_home:-$HOME}"
  commit=$(cd "$REPO_ROOT" && git rev-parse --short HEAD 2>/dev/null || echo none)
  version=dev
  date=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  pkg="github.com/CryptoLabInc/rune-admin/vault/internal/commands"

  local ldflags="-X '${pkg}.buildVersion=${version}' -X '${pkg}.buildCommit=${commit}' -X '${pkg}.buildDate=${date}'"
  local out_rel="bin/runevault-${TARGET_OS}-${TARGET_ARCH}"

  mkdir -p "${user_home}/go/pkg/mod"
  mkdir -p "${REPO_ROOT}/vault/bin"
  [[ -n "${SUDO_USER:-}" ]] && chown "${SUDO_USER}" "${REPO_ROOT}/vault/bin"

  # Run docker as the invoking user so written files are owned correctly and
  # the user's go module cache is reused for speed.
  sudo -u "$build_user" -H docker run --rm \
    --platform "${TARGET_OS}/${TARGET_ARCH}" \
    -v "${REPO_ROOT}/vault:/src" \
    -v "${user_home}/go/pkg/mod:/go/pkg/mod" \
    -w /src \
    -e CGO_ENABLED=1 \
    -e LDFLAGS="$ldflags" \
    -e OUTPUT="$out_rel" \
    "${BUILDER_IMAGE}" \
    bash -c '
      set -e
      apt-get update -qq && apt-get install -y -qq libssl-dev >/dev/null
      go build -ldflags "$LDFLAGS" -o "$OUTPUT" ./cmd
    ' || die "Docker build failed."

  [[ -x "$LINUX_BINARY" ]] || die "Build did not produce ${LINUX_BINARY}."
  success "Built: ${LINUX_BINARY}"
}

# ── Local config prompts (mirror dev_csp_prompt_config) ───────────────────────
dev_local_prompt_config() {
  if [[ "$NON_INTERACTIVE" -eq 0 ]]; then
    printf '\n'
    printf '══════════════════════════════════════════════════════════\n'
    printf '  Local install configuration (dev mode)\n'
    printf '══════════════════════════════════════════════════════════\n'
    printf '\n'

    _prompt RUNEVAULT_TEAM_NAME         "Team name"         "devteam"
    _prompt RUNEVAULT_ENVECTOR_ENDPOINT "enVector endpoint" ""
    _prompt RUNEVAULT_ENVECTOR_API_KEY  "enVector API key"  ""
    printf '\n'

    [[ -n "${RUNEVAULT_ENVECTOR_ENDPOINT:-}" ]] || die "enVector endpoint is required."
    [[ -n "${RUNEVAULT_ENVECTOR_API_KEY:-}" ]]  || die "enVector API key is required."
  else
    RUNEVAULT_TEAM_NAME="${RUNEVAULT_TEAM_NAME:-devteam}"
    RUNEVAULT_ENVECTOR_ENDPOINT="${RUNEVAULT_ENVECTOR_ENDPOINT:-https://envector.example.com}"
    RUNEVAULT_ENVECTOR_API_KEY="${RUNEVAULT_ENVECTOR_API_KEY:-dev-api-key-placeholder}"
  fi
}

# ── Local install branch ──────────────────────────────────────────────────────
dev_local_install() {
  dev_build_local_binary
  dev_local_prompt_config

  export RUNEVAULT_LOCAL_BINARY="$LOCAL_BINARY_HOST"
  export RUNEVAULT_SKIP_VERIFY=1
  export RUNEVAULT_TEAM_NAME
  export RUNEVAULT_ENVECTOR_ENDPOINT
  export RUNEVAULT_ENVECTOR_API_KEY

  if [[ -n "$PREFIX" ]]; then
    export RUNEVAULT_INSTALL_PREFIX="$PREFIX"
    export RUNEVAULT_BINARY_PATH="${PREFIX}/runevault"
    export RUNEVAULT_SKIP_SERVICE=1
  fi

  exec bash "${REPO_ROOT}/install.sh" --target local "${PASSTHROUGH_ARGS[@]+"${PASSTHROUGH_ARGS[@]}"}"
}

# ── Uninstall forward ─────────────────────────────────────────────────────────
# install-dev.sh defers all uninstall logic to install.sh. install.sh handles
# both local (service + files) and CSP (terraform destroy + dir cleanup).
dev_forward_uninstall() {
  info "Forwarding uninstall to install.sh (target: ${TARGET})..."
  local args=(--uninstall --target "$TARGET")
  [[ -n "$INSTALL_DIR_CSP" ]] && args+=(--install-dir "$INSTALL_DIR_CSP")
  [[ "$NON_INTERACTIVE" -eq 1 ]] && args+=(--non-interactive)

  if [[ "$TARGET" = "local" && -n "$PREFIX" ]]; then
    export RUNEVAULT_INSTALL_PREFIX="$PREFIX"
    export RUNEVAULT_BINARY_PATH="${PREFIX}/runevault"
  fi

  exec bash "${REPO_ROOT}/install.sh" "${args[@]}"
}

# ── CSP preflight (mirror install.sh:228–285) ─────────────────────────────────
dev_csp_preflight() {
  local csp=$1
  info "Running CSP preflight checks for ${csp}..."

  command -v terraform >/dev/null 2>&1 \
    || die "terraform is not installed. Install it (https://developer.hashicorp.com/terraform/install) and retry."

  local csp_cli auth_cmd auth_setup
  case "$csp" in
    aws)
      csp_cli=aws
      auth_cmd='aws sts get-caller-identity'
      auth_setup='aws configure'
      ;;
    gcp)
      csp_cli=gcloud
      auth_cmd='gcloud auth application-default print-access-token'
      auth_setup='gcloud auth application-default login'
      ;;
    oci)
      csp_cli=oci
      auth_cmd='oci iam region list'
      auth_setup='oci setup config'
      ;;
  esac

  local tf_user="${SUDO_USER:-$(id -un)}"

  if ! sudo -u "$tf_user" -H bash -lc "command -v ${csp_cli}" >/dev/null 2>&1; then
    die "'${csp_cli}' CLI not found in PATH for user '${tf_user}'. Install it and re-run."
  fi

  if ! sudo -u "$tf_user" -H bash -lc "${auth_cmd}" >/dev/null 2>&1; then
    die "'${csp_cli}' is not authenticated for user '${tf_user}'. Authenticate and re-run: ${auth_setup}"
  fi

  success "CSP preflight passed."
}

# ── CSP config prompts (mirror install.sh:287–347) ────────────────────────────
dev_csp_prompt_config() {
  local csp=$1

  if [[ "$NON_INTERACTIVE" -eq 0 ]]; then
    printf '\n'
    printf '══════════════════════════════════════════════════════════\n'
    printf '  Cloud deployment configuration (dev mode)\n'
    printf '══════════════════════════════════════════════════════════\n'
    printf '\n'

    _prompt TEAM_NAME          "Team name"          "devteam"
    _prompt ENVECTOR_ENDPOINT  "enVector endpoint"  ""
    _prompt ENVECTOR_API_KEY   "enVector API key"   ""

    case "$csp" in
      aws) _prompt CSP_REGION "AWS region"   "us-east-1"   ;;
      gcp)
        _prompt CSP_REGION    "GCP region"   "us-central1"
        _prompt GCP_PROJECT_ID "GCP project ID" ""
        ;;
      oci)
        _prompt CSP_REGION         "OCI region"          "us-ashburn-1"
        _prompt OCI_COMPARTMENT_ID "OCI compartment OCID" ""
        ;;
    esac
    printf '\n'
  else
    TEAM_NAME="${RUNEVAULT_TEAM_NAME:-}"
    ENVECTOR_ENDPOINT="${RUNEVAULT_ENVECTOR_ENDPOINT:-}"
    ENVECTOR_API_KEY="${RUNEVAULT_ENVECTOR_API_KEY:-}"
    CSP_REGION="${RUNEVAULT_CSP_REGION:-}"
    GCP_PROJECT_ID="${RUNEVAULT_GCP_PROJECT_ID:-}"
    OCI_COMPARTMENT_ID="${RUNEVAULT_OCI_COMPARTMENT_ID:-}"

    local missing=()
    [[ -z "$TEAM_NAME" ]]         && missing+=("RUNEVAULT_TEAM_NAME")
    [[ -z "$ENVECTOR_ENDPOINT" ]] && missing+=("RUNEVAULT_ENVECTOR_ENDPOINT")
    [[ -z "$ENVECTOR_API_KEY" ]]  && missing+=("RUNEVAULT_ENVECTOR_API_KEY")
    [[ "$csp" = gcp && -z "$GCP_PROJECT_ID" ]]      && missing+=("RUNEVAULT_GCP_PROJECT_ID")
    [[ "$csp" = oci && -z "$OCI_COMPARTMENT_ID" ]]  && missing+=("RUNEVAULT_OCI_COMPARTMENT_ID")
    if [[ ${#missing[@]} -gt 0 ]]; then
      printf 'ERROR: Missing required env vars:\n' >&2
      for v in "${missing[@]}"; do printf '  %s\n' "$v" >&2; done
      exit 1
    fi
  fi

  [[ -n "$TEAM_NAME" ]]          || die "Team name is required."
  [[ -n "$ENVECTOR_ENDPOINT" ]]  || die "enVector endpoint is required."
  [[ -n "$ENVECTOR_API_KEY" ]]   || die "enVector API key is required."
  if [[ "$csp" = gcp ]]; then
    [[ -n "$GCP_PROJECT_ID" ]]     || die "GCP project ID is required."
  fi
  if [[ "$csp" = oci ]]; then
    [[ -n "$OCI_COMPARTMENT_ID" ]] || die "OCI compartment OCID is required."
  fi
}

# ── SSH key (identical to install.sh:349–361) ─────────────────────────────────
dev_csp_generate_ssh_key() {
  local key_path="${INSTALL_DIR_CSP}/ssh_key"
  if [[ -f "$key_path" ]]; then
    info "SSH key already exists: ${key_path}"
    return 0
  fi
  ssh-keygen -t ed25519 -N '' -f "$key_path" -q
  chmod 0600 "$key_path"
  chmod 0644 "${key_path}.pub"
  [[ -n "${SUDO_USER:-}" ]] \
    && chown "${SUDO_USER}" "$key_path" "${key_path}.pub"
  success "SSH key generated: ${key_path}"
}

# ── Terraform files (mirror install.sh:373–399, swap to *-dev variants) ──────
dev_csp_copy_terraform_files() {
  local csp=$1
  local tf_src="${REPO_ROOT}/deployment/${csp}"
  local tf_dest="${INSTALL_DIR_CSP}/deployment"
  mkdir -p "$tf_dest"

  cp "${tf_src}/main.tf" "${tf_dest}/main.tf"

  # Use the *-dev variant of cloud-init / startup-script, but rename to the
  # canonical filename so main.tf's templatefile() reference keeps working
  # without Terraform changes.
  case "$csp" in
    aws)
      [[ -f "${tf_src}/cloud-init-dev.yaml" ]] \
        || die "Missing ${tf_src}/cloud-init-dev.yaml."
      cp "${tf_src}/cloud-init-dev.yaml" "${tf_dest}/cloud-init.yaml"
      ;;
    gcp|oci)
      [[ -f "${tf_src}/startup-script-dev.sh" ]] \
        || die "Missing ${tf_src}/startup-script-dev.sh."
      cp "${tf_src}/startup-script-dev.sh" "${tf_dest}/startup-script.sh"
      ;;
  esac

  printf '*.tfvars\nterraform.tfstate*\n.terraform/\n' > "${INSTALL_DIR_CSP}/.gitignore"
  [[ -n "${SUDO_USER:-}" ]] && chown -R "${SUDO_USER}" "$tf_dest" "${INSTALL_DIR_CSP}/.gitignore"
  success "Terraform files (dev variant) ready: ${tf_dest}"
}

# ── tfvars (mirror install.sh:403–439) ────────────────────────────────────────
dev_csp_render_tfvars() {
  local csp=$1
  local tf_dir="${INSTALL_DIR_CSP}/deployment"
  local tfvars="${tf_dir}/terraform.tfvars"
  local public_key=""

  if [[ -f "${tf_dir}/terraform.tfstate" ]]; then
    if [[ "$NON_INTERACTIVE" -eq 0 ]]; then
      local answer=n
      read -r -p "terraform.tfstate already exists in ${tf_dir}. Re-apply? [y/N] " answer
      [[ "$answer" =~ ^[Yy] ]] || { info "Aborted."; exit 0; }
    else
      warn "terraform.tfstate exists — re-applying (idempotent)."
    fi
  fi

  [[ -f "${INSTALL_DIR_CSP}/ssh_key.pub" ]] \
    && public_key=$(cat "${INSTALL_DIR_CSP}/ssh_key.pub")

  {
    printf 'team_name          = "%s"\n' "$(escape_tf "${TEAM_NAME:-default}")"
    printf 'tls_mode           = "self-signed"\n'
    printf 'envector_endpoint  = "%s"\n' "$(escape_tf "${ENVECTOR_ENDPOINT}")"
    printf 'envector_api_key   = "%s"\n' "$(escape_tf "${ENVECTOR_API_KEY}")"
    printf 'runevault_version  = "dev"\n'
    printf 'public_key         = "%s"\n' "$(escape_tf "${public_key}")"
    printf 'region             = "%s"\n' "$(escape_tf "${CSP_REGION}")"
    case "$csp" in
      gcp) printf 'project_id         = "%s"\n' "$(escape_tf "${GCP_PROJECT_ID}")" ;;
      oci) printf 'compartment_id     = "%s"\n' "$(escape_tf "${OCI_COMPARTMENT_ID}")" ;;
    esac
  } > "$tfvars"

  chmod 0600 "$tfvars"
  [[ -n "${SUDO_USER:-}" ]] && chown "${SUDO_USER}" "$tfvars"
  success "terraform.tfvars written: ${tfvars}"
}

# ── Terraform apply (mirror install.sh:441–453) ───────────────────────────────
dev_csp_run_terraform() {
  local tf_dir="${INSTALL_DIR_CSP}/deployment"
  local tf_user="${SUDO_USER:-$(id -un)}"

  info "Running terraform init..."
  (cd "$tf_dir" && sudo -u "$tf_user" terraform init -input=false)
  info "Running terraform apply..."
  (cd "$tf_dir" && sudo -u "$tf_user" terraform apply -auto-approve -input=false)

  chmod 0600 "${tf_dir}/terraform.tfstate" 2>/dev/null || true
  chmod 0600 "${tf_dir}/terraform.tfstate.backup" 2>/dev/null || true
  success "Terraform apply complete."
}

# ── Upload + remote install (replaces csp_post_deploy for dev mode) ───────────
dev_csp_upload_and_install() {
  local csp=$1
  local tf_dir="${INSTALL_DIR_CSP}/deployment"
  local tf_user="${SUDO_USER:-$(id -un)}"
  local key_path="${INSTALL_DIR_CSP}/ssh_key"
  local ssh_user=ubuntu

  local public_ip
  public_ip=$(cd "$tf_dir" && sudo -u "$tf_user" terraform output -raw vault_public_ip 2>/dev/null) \
    || die "Could not read vault_public_ip from terraform output."
  CSP_PUBLIC_IP="$public_ip"

  local ssh_opts="-o BatchMode=yes -o StrictHostKeyChecking=no -o ConnectTimeout=15"
  local ssh_prefix="sudo -u ${tf_user}"

  # 1. Wait for SSH on the VM.
  info "Waiting for SSH on ${ssh_user}@${public_ip} (up to 30 min)..."
  local timeout_secs=1800
  local deadline=$(( $(date +%s) + timeout_secs ))
  local ssh_ready=0
  while [[ $(date +%s) -lt $deadline ]]; do
    # shellcheck disable=SC2086
    if $ssh_prefix ssh $ssh_opts -i "$key_path" "${ssh_user}@${public_ip}" true 2>/dev/null; then
      ssh_ready=1
      break
    fi
    sleep 15
  done
  [[ "$ssh_ready" -eq 1 ]] \
    || die "Timed out waiting for SSH. ssh -i ${key_path} ${ssh_user}@${public_ip}"
  success "SSH reachable."

  # 2. Wait for cloud-init-dev to finish (cosign is the last thing it installs).
  info "Waiting for cloud-init-dev to finish (cosign + apt prereqs)..."
  deadline=$(( $(date +%s) + 600 ))
  local prereqs_ready=0
  while [[ $(date +%s) -lt $deadline ]]; do
    # shellcheck disable=SC2086
    if $ssh_prefix ssh $ssh_opts -i "$key_path" "${ssh_user}@${public_ip}" \
         "test -x /usr/local/bin/cosign && test -x /usr/bin/openssl" 2>/dev/null; then
      prereqs_ready=1
      break
    fi
    sleep 10
  done
  [[ "$prereqs_ready" -eq 1 ]] \
    || die "Timed out waiting for cloud-init-dev. SSH in to debug: ssh -i ${key_path} ${ssh_user}@${public_ip}"
  success "Cloud-init-dev complete."

  # 3. SCP install.sh + linux/amd64 binary to /tmp.
  info "Uploading install.sh and runevault binary to ${public_ip}..."
  # shellcheck disable=SC2086
  $ssh_prefix scp $ssh_opts -i "$key_path" \
    "${REPO_ROOT}/install.sh" \
    "${LINUX_BINARY}" \
    "${ssh_user}@${public_ip}:/tmp/" \
    || die "SCP upload failed."
  success "Artifacts uploaded."

  # 4. Run install.sh on the VM with dev hooks.
  info "Running install.sh on the VM..."
  local tn ee ek
  tn=$(escape_single "$TEAM_NAME")
  ee=$(escape_single "$ENVECTOR_ENDPOINT")
  ek=$(escape_single "$ENVECTOR_API_KEY")
  local remote_cmd
  remote_cmd="sudo \
    RUNEVAULT_LOCAL_BINARY=/tmp/runevault-${TARGET_OS}-${TARGET_ARCH} \
    RUNEVAULT_SKIP_VERIFY=1 \
    RUNEVAULT_TEAM_NAME='${tn}' \
    RUNEVAULT_ENVECTOR_ENDPOINT='${ee}' \
    RUNEVAULT_ENVECTOR_API_KEY='${ek}' \
    bash /tmp/install.sh --target local --non-interactive --version dev"

  # shellcheck disable=SC2086
  $ssh_prefix ssh $ssh_opts -i "$key_path" "${ssh_user}@${public_ip}" "$remote_cmd" \
    || die "Remote install.sh failed. SSH in to debug: ssh -i ${key_path} ${ssh_user}@${public_ip}"
  success "Remote install complete."

  # 5. Pull CA cert back to the operator workstation.
  info "Fetching CA certificate..."
  mkdir -p "${INSTALL_DIR_CSP}/certs"
  [[ -n "${SUDO_USER:-}" ]] && chown "${SUDO_USER}" "${INSTALL_DIR_CSP}/certs"
  # shellcheck disable=SC2086
  $ssh_prefix scp $ssh_opts -i "$key_path" \
    "${ssh_user}@${public_ip}:/opt/runevault/certs/ca.pem" \
    "${INSTALL_DIR_CSP}/certs/ca.pem" \
    || die "CA cert fetch failed."
  success "CA certificate saved: ${INSTALL_DIR_CSP}/certs/ca.pem"
}

# ── Summary (mirror install.sh:491–518 + dev banner) ──────────────────────────
dev_csp_summary() {
  local csp=$1
  local tf_dir="${INSTALL_DIR_CSP}/deployment"
  local key_path="${INSTALL_DIR_CSP}/ssh_key"
  local public_ip="${CSP_PUBLIC_IP:-<unknown>}"
  local commit
  commit=$(cd "$REPO_ROOT" && git rev-parse --short HEAD 2>/dev/null || echo unknown)

  printf '\n'
  success "Rune-Vault deployed to $(printf '%s' "$csp" | tr 'a-z' 'A-Z') (dev mode)."
  printf '\n'
  printf '  Endpoint:  %s:%s\n' "$public_ip" "$GRPC_PORT"
  printf '  CA cert:   %s\n'    "${INSTALL_DIR_CSP}/certs/ca.pem"
  printf '  SSH:       ssh -i %s ubuntu@%s\n' "$key_path" "$public_ip"
  printf '  Terraform: %s\n'    "$tf_dir"
  printf '  Source:    local working tree (commit %s)\n' "$commit"
  printf '\n'
  printf 'Tear down:\n'
  printf '  cd %s && terraform destroy -auto-approve\n' "$tf_dir"
  printf '\n'
  printf 'Next steps (SSH into the VM, then run on the VM):\n'
  printf '  ssh -i %s ubuntu@%s\n' "$key_path" "$public_ip"
  printf '\n'
  printf '  Issue a token:  runevault token issue --user <name> --role member\n'
  printf '  Check status:   runevault status\n'
  printf '  View logs:      runevault logs\n'
  printf '  Manage daemon:  sudo systemctl start|stop|restart runevault\n'
  printf '\n'
  warn "BACKUP: Keep this safe — it cannot be recovered if lost:"
  warn "  Terraform state: ${tf_dir}/terraform.tfstate"
}

# ── CSP dispatch (mirror install.sh:520–549) ──────────────────────────────────
dev_csp_dispatch() {
  local csp="$TARGET"
  local user_home="${SUDO_USER:+$(eval echo ~"${SUDO_USER}")}"
  user_home="${user_home:-$HOME}"
  INSTALL_DIR_CSP="${INSTALL_DIR_CSP:-${user_home}/rune-vault-${csp}}"
  mkdir -p "$INSTALL_DIR_CSP"
  [[ -n "${SUDO_USER:-}" ]] && chown "${SUDO_USER}" "$INSTALL_DIR_CSP"

  dev_csp_preflight "$csp"
  dev_csp_prompt_config "$csp"
  dev_csp_generate_ssh_key
  dev_build_linux_binary
  dev_csp_copy_terraform_files "$csp"
  dev_csp_render_tfvars "$csp"
  dev_csp_run_terraform
  dev_csp_upload_and_install "$csp"
  dev_csp_summary "$csp"
  exit 0
}

# ── Main ───────────────────────────────────────────────────────────────────────
print_banner
resolve_target

[[ "$UNINSTALL" -eq 1 ]] && dev_forward_uninstall

dev_preflight

if [[ "$TARGET" = "local" ]]; then
  dev_local_install
else
  dev_csp_dispatch
fi
