#!/usr/bin/env bash
# Generate Python protobuf/gRPC stubs from .proto files.
#
# Prerequisites:
#   brew install bufbuild/buf/buf   (or: https://buf.build/docs/installation)
#   pip install grpcio-tools
#
# Usage:
#   ./scripts/proto-gen.sh          (from vault/ directory)
#   make proto-gen                  (via Makefile)
set -euo pipefail
cd "$(dirname "$0")/.."

# ── 1. Resolve buf dependencies ──────────────────────────────────────
echo "[proto-gen] Updating buf dependencies..."
buf dep update

# ── 2. Export protovalidate protos to temp dir ───────────────────────
DEPS_DIR=$(mktemp -d)
trap 'rm -rf "$DEPS_DIR"' EXIT

echo "[proto-gen] Exporting protovalidate protos..."
buf export buf.build/bufbuild/protovalidate -o "$DEPS_DIR"

# ── 3. Locate google well-known protos bundled with grpcio-tools ────
GRPC_PROTO=$(python3 -c "
import grpc_tools, os
print(os.path.join(os.path.dirname(grpc_tools.__file__), '_proto'))
")

# ── 4. Generate vault_service stubs ─────────────────────────────────
echo "[proto-gen] Generating vault_service_pb2.py / vault_service_pb2_grpc.py..."
python3 -m grpc_tools.protoc \
  -Iproto \
  -I"$DEPS_DIR" \
  -I"$GRPC_PROTO" \
  --python_out=proto \
  --grpc_python_out=proto \
  proto/vault_service.proto

# ── 5. Generate protovalidate runtime stubs ──────────────────────────
echo "[proto-gen] Generating buf/validate/validate_pb2.py..."
python3 -m grpc_tools.protoc \
  -I"$DEPS_DIR" \
  -I"$GRPC_PROTO" \
  --python_out=. \
  "$DEPS_DIR/buf/validate/validate.proto"

mkdir -p buf/validate
touch buf/__init__.py buf/validate/__init__.py

echo "[proto-gen] Done."
