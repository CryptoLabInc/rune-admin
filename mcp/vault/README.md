# Mocked Rune-Vault (eV^2)

This directory contains the implementation of a Mocked Rune-Vault running as an **MCP Server**.
It acts as a Trusted Client responsible for holding Secret Keys and performing sensitive decryption operations.

## Architecture

*   **Vault**: An MCP Server (Python) that holds the `SecKey.json`.
*   **Agent**: An AI Agent (or client script) that retrieves the Public Key (`EncKey.json`), encrypts queries, and requests Score Decryption.
*   **Security**: The Agent NEVER sees the `SecKey`. It requires the Vault to decrypt scores.

## Setup

1.  **Environment**: Ensure `.vault_venv` is created and has dependencies.
    ```bash
    python3.12 -m venv ../../.vault_venv
    source ../../.vault_venv/bin/activate
    pip install pyenvector mcp uvicorn numpy
    ```
    *(Note: pyenvector must be accessible/installed)*

2.  **Generate Keys**:
    Keys (`EncKey.json`, `SecKey.json`, etc.) are generated automatically in `vault_keys/` on first run.

## Usage

### 1. Run the Server
Start the Vault MCP Server on port 50080 (SSE Transport):
```bash
./run_vault.sh
```

### 2. Run the Demo
Run the local demo script which simulates an Agent interacting with the Vault logic (in-process for simplicity, mimicking the MCP tool calls):
```bash
source ../../.vault_venv/bin/activate
python3 demo_local.py
```

## Authentication
This Vault requires simple Token-based authentication.
*   **Valid Tokens**: `envector-team-alpha`, `envector-admin-001`
*   **Mechanism**: Pass `token` string argument to every tool.
*   *Note: In a real deployment, tokens would be validated against a database or OAuth provider.*

## MCP Tools

The server exposes the following tools to any MCP Client (Claude, etc.):

### `get_public_key(token)`
Returns the public key bundle (`EncKey`, `EvalKey`, `MetadataKey`).
*   **Args**:
    *   `token` (str): Valid authentication token.
*   **Returns**: `str` (JSON object)
    ```json
    {
        "EncKey.json": "...",
        "EvalKey.json": "...",
        "MetadataKey.json": "..."
    }
    ```

### `decrypt_scores(token, encrypted_blob_b64, top_k=5)`
Decrypts a blob of encrypted scores and returns the Top-K results.
*   **Args**:
    *   `token` (str): Valid authentication token.
    *   `encrypted_blob_b64` (str): Base64 encoded string of the serialized Encrypted Result (CipherBlock/Query).
    *   `top_k` (int): Number of results to return (default 5, max 10).
*   **Returns**: `str` (JSON string list of `{"index": int, "score": float}`)

## Implementation Details
*   **Crypto**: Uses real `pyenvector` SDK.
*   **Transport**: Uses `Query.serialize()` / `Query.deserializeFrom()` for handling FHE objects over the wire (wrapped in Base64).
