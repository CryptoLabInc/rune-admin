# Rune-Vault

This directory contains the Rune-Vault server — a **gRPC service** that holds FHE secret keys and performs all decryption operations.

## Architecture

*   **Vault**: A gRPC server (port 50051) with a metrics/health HTTP endpoint (port 9090) that holds `SecKey.json`.
*   **envector-mcp-server**: Communicates with Vault via **gRPC** for key fetching and decryption (the `remember` pipeline).
*   **Security**: The Agent NEVER sees the secret key. All decryption is delegated to Vault.

## Quick Start (Docker)

### 1. Configure Environment

```bash
cp .env.example .env
```

Edit `.env` and fill in the required values:

| Variable | Required | Description |
|---|---|---|
| `VAULT_TOKENS` | Yes | Auth tokens for clients (comma-separated) |
| `NGROK_AUTHTOKEN` | For tunneling | ngrok authtoken for public gRPC endpoint |
| `ENVECTOR_ENDPOINT` | For team index | enVector cluster endpoint |
| `ENVECTOR_API_KEY` | For team index | enVector API key |
| `VAULT_INDEX_NAME` | No | Team index name (default: `runecontext`) |
| `EMBEDDING_DIM` | No | Embedding dimension (default: `384`) |

### 2. Build & Run

```bash
# Build image and start Vault
docker compose up -d vault

# Check logs
docker logs -f rune-vault
```

FHE keys (`EncKey.json`, `SecKey.json`, etc.) are generated automatically on first run and persisted in the `vault-keys` Docker volume.

### 3. Expose gRPC via ngrok (optional)

To make the gRPC endpoint publicly accessible:

```bash
# Start both Vault and ngrok
docker compose up -d

# Check assigned TCP address
curl -s http://localhost:4040/api/tunnels | python3 -m json.tool
```

The assigned address (e.g. `0.tcp.jp.ngrok.io:12345`) is your public gRPC endpoint.

### 4. Verify

```bash
# Health check (HTTP)
curl http://localhost:9090/health

# gRPC health check
grpcurl -plaintext localhost:50051 grpc.health.v1.Health/Check
```

### Stop / Restart

```bash
docker compose down          # stop and remove containers
docker compose up -d         # start (re-reads .env)
docker compose build         # rebuild after code changes
```

> **Note:** `docker compose restart` does NOT re-read `.env`. Always use `down` then `up -d` when changing environment variables.

## Local Development (without Docker)

<details>
<summary>Click to expand</summary>

```bash
python3.12 -m venv ../.vault_venv
source ../.vault_venv/bin/activate
pip install -r requirements.txt

python3 vault_grpc_server.py --host 0.0.0.0 --grpc-port 50051 --metrics-port 9090
```

</details>

## Authentication
This Vault requires simple Token-based authentication.
*   **Valid Tokens**: Configured via `VAULT_TOKENS` environment variable (comma-separated).
*   **Mechanism**: Token passed per gRPC request message field.
*   *Note: In a real deployment, tokens would be validated against a database or OAuth provider.*

## gRPC Service (used by envector-mcp-server)

Defined in `proto/vault_service.proto` (`rune.vault.v1.VaultService`):

### `GetPublicKey(token)` → `GetPublicKeyResponse`
Returns the public key bundle (`EncKey`, `EvalKey`, optional team index name).

### `DecryptScores(token, encrypted_blob_b64, top_k)` → `DecryptScoresResponse`
Decrypts FHE-encrypted similarity scores and returns typed `ScoreEntry` messages (shard_idx, row_idx, score).
*   **top_k**: Max 10, enforced by Vault policy.

### `DecryptMetadata(token, encrypted_metadata_list)` → `DecryptMetadataResponse`
Decrypts AES-encrypted metadata strings using Vault's MetadataKey.

### Health Check
Standard `grpc.health.v1.Health` protocol on port 50051.

## Implementation Details
*   **Crypto**: Uses real `pyenvector` SDK (FHE).
*   **Transport**: gRPC with protobuf.
*   **Max message size**: 256 MB (EvalKey can be tens of MB).
*   **Monitoring**: Prometheus metrics at `:9090/metrics`, health check at `:9090/health`.
