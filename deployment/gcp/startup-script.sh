#!/bin/bash
set -euo pipefail

# Rune-Vault Startup Script for GCP Compute Engine
# Deploys Docker-based Vault with gRPC (port 50051)

exec > /var/log/rune-vault-startup.log 2>&1
echo "=== Rune-Vault startup script began at $(date) ==="

# Install packages
apt-get update
apt-get install -y ca-certificates curl gnupg jq openssl

# Create Rune directory structure
mkdir -p /opt/rune/certs /opt/rune/backups /opt/rune/logs /opt/rune/config
chmod 700 /opt/rune/certs

# Write docker-compose.yml
cat > /opt/rune/docker-compose.yml <<'COMPOSE'
services:
  vault:
    image: ghcr.io/cryptolabinc/rune-vault:latest
    container_name: rune-vault
    restart: unless-stopped
    ports:
      - "0.0.0.0:50051:50051"
      - "127.0.0.1:9090:9090"
    env_file:
      - .env
    environment:
      - VAULT_TEAM_SECRET=${team_secret}
      - VAULT_INDEX_NAME=${vault_index_name}
      - ENVECTOR_ENDPOINT=${envector_endpoint}
      - ENVECTOR_API_KEY=${envector_api_key}
      - EMBEDDING_DIM=384
    volumes:
      - vault-keys:/app/vault_keys:rw
      - ./config:/app/config:rw
      - ./certs:/app/certs:rw
      - ./backups:/secure/backups:rw
      - ./logs:/var/log/rune-vault:rw
    healthcheck:
      test: ["CMD", "python3", "-c", "import urllib.request; urllib.request.urlopen('http://localhost:9090/health')"]
      interval: 30s
      timeout: 10s
      retries: 3
    security_opt:
      - no-new-privileges:true
    deploy:
      resources:
        limits:
          memory: 1G
          cpus: "1.0"
        reservations:
          memory: 512M
          cpus: "0.5"

volumes:
  vault-keys:
COMPOSE

# Write .env file
cat > /opt/rune/.env <<'ENVFILE'
VAULT_TLS_CERT=${tls_mode == "none" ? "" : "/app/certs/server.pem"}
VAULT_TLS_KEY=${tls_mode == "none" ? "" : "/app/certs/server.key"}
VAULT_TLS_DISABLE=${tls_mode == "none" ? "true" : ""}
ENVFILE
chmod 600 /opt/rune/.env

# Install Docker CE with compose plugin (v2)
install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
chmod a+r /etc/apt/keyrings/docker.asc
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo "$VERSION_CODENAME") stable" > /etc/apt/sources.list.d/docker.list
apt-get update
apt-get install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin

# Start Docker
systemctl enable docker
systemctl start docker

# Generate per-user token auth config files
cat > /opt/rune/config/vault-roles.yml <<'ROLES'
roles:
  admin:
    scope: [get_public_key, decrypt_scores, decrypt_metadata, manage_tokens]
    top_k: 50
    rate_limit: 150/60s
  member:
    scope: [get_public_key, decrypt_scores, decrypt_metadata]
    top_k: 10
    rate_limit: 30/60s
ROLES
echo "tokens: []" > /opt/rune/config/vault-tokens.yml
chmod 600 /opt/rune/config/vault-roles.yml /opt/rune/config/vault-tokens.yml

# Set up runevault CLI alias for ubuntu user
if ! grep -q 'alias runevault=' /home/ubuntu/.bashrc 2>/dev/null; then
  echo "alias runevault='docker exec -it rune-vault python3 /app/vault_admin_cli.py'" >> /home/ubuntu/.bashrc
fi
usermod -aG docker ubuntu 2>/dev/null || true

# TLS setup
if [ "${tls_mode}" = "self-signed" ]; then
  CERT_DIR="/opt/rune/certs"
  PUBLIC_IP=$(curl -4 -sf --connect-timeout 5 ifconfig.me 2>/dev/null || true)
  openssl genrsa -out "$CERT_DIR/ca.key" 4096 2>/dev/null
  openssl req -new -x509 -key "$CERT_DIR/ca.key" -out "$CERT_DIR/ca.pem" \
    -days 3650 -subj "/CN=Rune-Vault CA" -sha256
  openssl genrsa -out "$CERT_DIR/server.key" 2048 2>/dev/null
  TMPCONF=$(mktemp)
  printf '%s\n' \
    '[req]' \
    'distinguished_name = req_dn' \
    'req_extensions = v3_req' \
    'prompt = no' \
    '[req_dn]' \
    'CN = localhost' \
    '[v3_req]' \
    'subjectAltName = @alt_names' \
    '[alt_names]' \
    'DNS.1 = localhost' \
    'DNS.2 = vault' \
    'DNS.3 = rune-vault' \
    'IP.1  = 127.0.0.1' \
    > "$TMPCONF"
  TLS_HOSTNAME="${tls_hostname}"
  if [ -n "$TLS_HOSTNAME" ]; then
    echo "DNS.4 = $TLS_HOSTNAME" >> "$TMPCONF"
  fi
  if [ -n "$PUBLIC_IP" ]; then
    echo "IP.2  = $PUBLIC_IP" >> "$TMPCONF"
  fi
  openssl req -new -key "$CERT_DIR/server.key" -out "$CERT_DIR/server.csr" -config "$TMPCONF"
  openssl x509 -req -in "$CERT_DIR/server.csr" \
    -CA "$CERT_DIR/ca.pem" -CAkey "$CERT_DIR/ca.key" -CAcreateserial \
    -out "$CERT_DIR/server.pem" -days 825 -sha256 \
    -extfile "$TMPCONF" -extensions v3_req 2>/dev/null
  rm -f "$TMPCONF" "$CERT_DIR/server.csr" "$CERT_DIR/ca.srl"
  chmod 600 "$CERT_DIR/ca.key" "$CERT_DIR/server.key"
  chmod 644 "$CERT_DIR/ca.pem" "$CERT_DIR/server.pem"
fi

# Pull with retry and start Rune-Vault
cd /opt/rune
for i in 1 2 3 4 5; do
  docker compose pull && break
  echo "Docker pull retry $i..." && sleep 10
done
docker compose up -d

# Wait for Vault to be ready
sleep 10
timeout 300 bash -c 'until curl -sf http://localhost:9090/health; do sleep 2; done'

echo "=== Rune-Vault startup script completed at $(date) ==="
