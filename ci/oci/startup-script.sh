#!/bin/bash
set -euo pipefail

# GitHub Actions Self-Hosted Runner Bootstrap for OCI Compute
# Installs: Docker CE, GitHub Actions runner
# Dev tools (Python, buf, ruff) are managed by mise via jdx/mise-action in CI
# Runner labels: self-hosted, ${runner_labels}

exec > /var/log/ci-runner-startup.log 2>&1
echo "=== CI runner startup script began at $(date) ==="

# ── Wait for cloud-init apt operations to release locks ─────────────
echo "Waiting for apt locks to be released..."
while fuser /var/lib/dpkg/lock-frontend /var/lib/apt/lists/lock /var/cache/apt/archives/lock >/dev/null 2>&1; do
  sleep 5
done

# ── System packages (retry on transient mirror errors) ──────────────
for i in $(seq 1 30); do
  apt-get update -q \
    && apt-get -y --fix-broken install \
    && apt-get install -y ca-certificates curl gnupg jq openssl git \
    && break
  echo "apt retry $i..." && sleep 10
done

# ── Docker CE with compose plugin (v2) ──────────────────────────────
install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
chmod a+r /etc/apt/keyrings/docker.asc
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo "$VERSION_CODENAME") stable" > /etc/apt/sources.list.d/docker.list
apt-get update
apt-get install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin

systemctl enable docker
systemctl start docker

# ── GitHub Actions Runner ───────────────────────────────────────────
RUNNER_VERSION="2.322.0"
RUNNER_USER="runner"
RUNNER_HOME="/opt/actions-runner"

useradd -m -s /bin/bash "$RUNNER_USER"
usermod -aG docker "$RUNNER_USER"

mkdir -p "$RUNNER_HOME"
cd "$RUNNER_HOME"

curl -sSL "https://github.com/actions/runner/releases/download/v$${RUNNER_VERSION}/actions-runner-linux-x64-$${RUNNER_VERSION}.tar.gz" \
  | tar -xz

chown -R "$RUNNER_USER":"$RUNNER_USER" "$RUNNER_HOME"

# Configure runner (non-interactive)
su - "$RUNNER_USER" -c "cd $RUNNER_HOME && ./config.sh \
  --url https://github.com/${github_repo} \
  --token ${github_runner_token} \
  --labels self-hosted,${runner_labels} \
  --name vault-ci-runner \
  --work _work \
  --unattended \
  --replace"

# Runner environment configuration
echo "RUNNER_WORKER_COUNT=3" >> "$RUNNER_HOME/.env"
echo "FORCE_JAVASCRIPT_ACTIONS_TO_NODE24=true" >> "$RUNNER_HOME/.env"

# Install and start as systemd service
cd "$RUNNER_HOME"
./svc.sh install "$RUNNER_USER"
./svc.sh start

echo "=== CI runner startup script completed at $(date) ==="
