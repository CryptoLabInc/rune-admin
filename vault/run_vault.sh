#!/bin/bash
# Get the directory of this script
DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
WORKSPACE_ROOT="$DIR/.."
VENV_PATH="$WORKSPACE_ROOT/.vault_venv"

if [ -f "$VENV_PATH/bin/activate" ]; then
    source "$VENV_PATH/bin/activate"
else
    echo "Virtual environment not found at $VENV_PATH. Please create it first."
    exit 1
fi

echo "Starting Rune-Vault gRPC server on port 50051..."
python3 "$DIR/vault_grpc_server.py" --host 0.0.0.0 --grpc-port 50051 --metrics-port 9090
