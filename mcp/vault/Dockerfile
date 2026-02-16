FROM python:3.12-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
        gcc \
        python3-dev \
    && rm -rf /var/lib/apt/lists/*

RUN useradd -m -u 1000 vault

WORKDIR /app

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY vault_mcp.py vault_grpc_server.py monitoring.py ./
COPY proto/ proto/

RUN mkdir -p /app/vault_keys /secure/backups /var/log/vault-mcp \
    && chown -R vault:vault /app /secure /var/log/vault-mcp

USER vault

EXPOSE 50080 50051

HEALTHCHECK --interval=30s --timeout=10s --retries=3 \
    CMD python3 -c "import urllib.request; urllib.request.urlopen('http://localhost:50080/health')" || exit 1

CMD ["python3", "vault_mcp.py", "server", "--host", "0.0.0.0", "--port", "50080", "--grpc-port", "50051"]
