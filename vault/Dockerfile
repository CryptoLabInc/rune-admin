# ── Stage 1: Generate proto stubs ────────────────────────────────────
FROM python:3.12-slim AS proto-builder

RUN apt-get update && apt-get install -y --no-install-recommends curl \
    && rm -rf /var/lib/apt/lists/*

# Install buf CLI
RUN curl -sSL "https://github.com/bufbuild/buf/releases/latest/download/buf-Linux-$(uname -m)" \
        -o /usr/local/bin/buf \
    && chmod +x /usr/local/bin/buf

RUN pip install --no-cache-dir "grpcio-tools>=1.60.2,<=1.71.2" "protobuf>=5.29.0,<6"

WORKDIR /build
COPY buf.yaml buf.lock ./
COPY proto/vault_service.proto proto/__init__.py proto/
COPY scripts/proto-gen.sh scripts/
RUN bash scripts/proto-gen.sh

# ── Stage 2: Runtime ─────────────────────────────────────────────────
FROM python:3.12-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
        gcc \
        python3-dev \
        openssl \
        gosu \
    && rm -rf /var/lib/apt/lists/*

RUN useradd -m -u 1000 vault

WORKDIR /app

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY vault_core.py vault_grpc_server.py monitoring.py token_store.py admin_server.py vault_admin_cli.py request_validator.py validation_interceptor.py ./
COPY --from=proto-builder /build/proto/ proto/
COPY --from=proto-builder /build/buf/ buf/
COPY docker-entrypoint.sh .
RUN chmod +x docker-entrypoint.sh vault_admin_cli.py

RUN mkdir -p /app/vault_keys /app/certs /app/config /secure/backups /var/log/rune-vault \
    && chown -R vault:vault /app /secure /var/log/rune-vault

ENV PYTHONPATH=/app/proto:/app

EXPOSE 50051 9090

HEALTHCHECK --interval=30s --timeout=10s --retries=3 \
    CMD python3 -c "import urllib.request; urllib.request.urlopen('http://localhost:9090/health')" || exit 1

ENTRYPOINT ["./docker-entrypoint.sh"]
CMD ["--host", "0.0.0.0", "--grpc-port", "50051", "--metrics-port", "9090"]
