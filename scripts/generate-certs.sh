#!/usr/bin/env bash
#
# Generate self-signed CA + server certificate for Rune-Vault TLS.
#
# Usage:
#   ./scripts/generate-certs.sh [output-dir] [hostname]
#
# Outputs:
#   <output-dir>/ca.pem       — CA certificate (distribute to clients)
#   <output-dir>/ca.key       — CA private key (keep secret)
#   <output-dir>/server.pem   — Server certificate
#   <output-dir>/server.key   — Server private key
#
# The certificate SAN automatically includes:
#   - localhost, vault, rune-vault, 127.0.0.1 (always)
#   - <hostname> argument (if provided)
#   - Public IP via ifconfig.me (auto-detected)

set -euo pipefail

OUTPUT_DIR="${1:-vault/certs}"
HOSTNAME="${2:-localhost}"

CA_DAYS=3650      # 10 years
SERVER_DAYS=825   # ~2.25 years (Apple max)

# Auto-detect public IP
echo "==> Detecting public IP..."
PUBLIC_IP=$(curl -4 -sf --connect-timeout 5 ifconfig.me 2>/dev/null || true)
if [ -n "$PUBLIC_IP" ]; then
    echo "    Public IP: $PUBLIC_IP"
else
    echo "    Could not detect public IP (skipping)"
fi

mkdir -p "$OUTPUT_DIR"

echo "==> Generating CA key (4096-bit)..."
openssl genrsa -out "$OUTPUT_DIR/ca.key" 4096 2>/dev/null

echo "==> Generating CA certificate..."
openssl req -new -x509 \
    -key "$OUTPUT_DIR/ca.key" \
    -out "$OUTPUT_DIR/ca.pem" \
    -days "$CA_DAYS" \
    -subj "/CN=Rune-Vault CA" \
    -sha256

echo "==> Generating server key (2048-bit)..."
openssl genrsa -out "$OUTPUT_DIR/server.key" 2048 2>/dev/null

echo "==> Generating server certificate..."
# Create temporary config for SAN
TMPCONF=$(mktemp)
cat > "$TMPCONF" <<EOF
[req]
distinguished_name = req_dn
req_extensions = v3_req
prompt = no

[req_dn]
CN = ${HOSTNAME}

[v3_req]
subjectAltName = @alt_names

[alt_names]
DNS.1 = localhost
DNS.2 = vault
DNS.3 = rune-vault
DNS.4 = ${HOSTNAME}
IP.1  = 127.0.0.1
$([ -n "$PUBLIC_IP" ] && echo "IP.2  = $PUBLIC_IP")
EOF

openssl req -new \
    -key "$OUTPUT_DIR/server.key" \
    -out "$OUTPUT_DIR/server.csr" \
    -config "$TMPCONF"

openssl x509 -req \
    -in "$OUTPUT_DIR/server.csr" \
    -CA "$OUTPUT_DIR/ca.pem" \
    -CAkey "$OUTPUT_DIR/ca.key" \
    -CAcreateserial \
    -out "$OUTPUT_DIR/server.pem" \
    -days "$SERVER_DAYS" \
    -sha256 \
    -extfile "$TMPCONF" \
    -extensions v3_req \
    2>/dev/null

# Clean up temp files
rm -f "$TMPCONF" "$OUTPUT_DIR/server.csr" "$OUTPUT_DIR/ca.srl"

# Restrict private key permissions
chmod 600 "$OUTPUT_DIR/ca.key" "$OUTPUT_DIR/server.key"
chmod 644 "$OUTPUT_DIR/ca.pem" "$OUTPUT_DIR/server.pem"

echo ""
echo "Certificates generated in: $OUTPUT_DIR/"
echo "  ca.pem      — CA certificate (distribute to clients for self-signed verification)"
echo "  ca.key      — CA private key (keep secret)"
echo "  server.pem  — Server certificate"
echo "  server.key  — Server private key"
echo ""
SAN_SUMMARY="localhost, vault, rune-vault, ${HOSTNAME}, 127.0.0.1"
[ -n "$PUBLIC_IP" ] && SAN_SUMMARY="${SAN_SUMMARY}, ${PUBLIC_IP}"
echo "Server cert SANs: ${SAN_SUMMARY}"
