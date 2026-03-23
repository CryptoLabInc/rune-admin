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

COPY vault_core.py vault_grpc_server.py monitoring.py token_store.py admin_server.py vault_admin_cli.py ./
COPY proto/ proto/
COPY docker-entrypoint.sh .
RUN chmod +x docker-entrypoint.sh vault_admin_cli.py

RUN mkdir -p /app/vault_keys /app/certs /app/config /secure/backups /var/log/rune-vault \
    && chown -R vault:vault /app /secure /var/log/rune-vault /var/run

EXPOSE 50051 9090

HEALTHCHECK --interval=30s --timeout=10s --retries=3 \
    CMD python3 -c "import urllib.request; urllib.request.urlopen('http://localhost:9090/health')" || exit 1

ENTRYPOINT ["./docker-entrypoint.sh"]
CMD ["--host", "0.0.0.0", "--grpc-port", "50051", "--metrics-port", "9090"]
