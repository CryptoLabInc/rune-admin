#!/bin/sh
#
# Docker entrypoint for Rune-Vault.
# Auto-generates self-signed certificates if none are provided.

set -e

CERT_DIR="/app/certs"

# Skip auto-generation if TLS is disabled
if [ "${VAULT_TLS_DISABLE:-}" = "true" ]; then
    echo "[entrypoint] TLS disabled — skipping certificate generation."
    chown -R vault:vault /app/vault_keys /app/config /secure /var/log/rune-vault 2>/dev/null || true
    exec gosu vault python3 vault_grpc_server.py "$@"
fi

# Auto-generate self-signed cert if no cert exists and env vars not set
if [ -z "${VAULT_TLS_CERT:-}" ] && [ ! -f "$CERT_DIR/server.pem" ]; then
    echo "[entrypoint] No TLS certificate found — generating self-signed cert..."
    mkdir -p "$CERT_DIR"

    # Generate CA
    openssl genrsa -out "$CERT_DIR/ca.key" 4096 2>/dev/null
    openssl req -new -x509 \
        -key "$CERT_DIR/ca.key" \
        -out "$CERT_DIR/ca.pem" \
        -days 3650 \
        -subj "/CN=Rune-Vault CA" \
        -sha256

    # Generate server cert with SANs
    openssl genrsa -out "$CERT_DIR/server.key" 2048 2>/dev/null

    TMPCONF=$(mktemp)
    cat > "$TMPCONF" <<EOF
[req]
distinguished_name = req_dn
req_extensions = v3_req
prompt = no

[req_dn]
CN = rune-vault

[v3_req]
subjectAltName = DNS:localhost,DNS:vault,DNS:rune-vault,IP:127.0.0.1
EOF

    openssl req -new \
        -key "$CERT_DIR/server.key" \
        -out "$CERT_DIR/server.csr" \
        -config "$TMPCONF"

    openssl x509 -req \
        -in "$CERT_DIR/server.csr" \
        -CA "$CERT_DIR/ca.pem" \
        -CAkey "$CERT_DIR/ca.key" \
        -CAcreateserial \
        -out "$CERT_DIR/server.pem" \
        -days 825 \
        -sha256 \
        -extfile "$TMPCONF" \
        -extensions v3_req \
        2>/dev/null

    rm -f "$TMPCONF" "$CERT_DIR/server.csr" "$CERT_DIR/ca.srl"
    chmod 600 "$CERT_DIR/ca.key" "$CERT_DIR/server.key"

    echo "[entrypoint] Self-signed certificates generated in $CERT_DIR/"
    echo "[entrypoint] Distribute ca.pem to clients for verification."
fi

# Default to auto-generated certs if env vars not set
export VAULT_TLS_CERT="${VAULT_TLS_CERT:-$CERT_DIR/server.pem}"
export VAULT_TLS_KEY="${VAULT_TLS_KEY:-$CERT_DIR/server.key}"

# Fix ownership on mounted volumes so the vault user can read them
chown -R vault:vault /app/certs /app/vault_keys /app/config /secure /var/log/rune-vault 2>/dev/null || true

# Drop privileges and run as vault user
exec gosu vault python3 vault_grpc_server.py "$@"
