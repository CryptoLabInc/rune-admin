# Rune-Vault

This directory contains the Rune-Vault server — a **dual-stack service** (gRPC + MCP) that holds FHE secret keys and performs all decryption operations.

## Architecture

*   **Vault**: A dual-stack server (gRPC on port 50051, MCP/HTTP on port 50080) that holds `SecKey.json`.
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
docker compose up -d vault-mcp

# Check logs
docker logs -f vault-mcp
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
curl http://localhost:50080/health

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
python3.12 -m venv ../../.vault_venv
source ../../.vault_venv/bin/activate
pip install -r requirements.txt

python3 vault_mcp.py server --host 0.0.0.0 --port 50080 --grpc-port 50051
```

</details>

## Authentication
This Vault requires simple Token-based authentication.
*   **Valid Tokens**: Configured via `VAULT_TOKENS` environment variable (comma-separated).
*   **Mechanism**: Token passed per request (gRPC message field or MCP tool argument).
*   *Note: In a real deployment, tokens would be validated against a database or OAuth provider.*

## gRPC Service (Primary — used by envector-mcp-server)

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

## MCP Tools (Legacy — kept for backward compatibility)

The same operations are also available as MCP tools on port 50080:

### `get_public_key(token)` / `decrypt_scores(token, encrypted_blob_b64, top_k)` / `decrypt_metadata(token, encrypted_metadata_list)`
Same as gRPC RPCs above, but over MCP/HTTP transport with JSON string returns.

## Implementation Details
*   **Crypto**: Uses real `pyenvector` SDK (FHE).
*   **Transport**: gRPC with protobuf (primary), MCP/HTTP with JSON (legacy).
*   **Max message size**: 256 MB (EvalKey can be tens of MB).
*   **Monitoring**: Prometheus metrics at `/metrics`, health check at `/health`.
