#!/bin/bash
set -euo pipefail
exec > /var/log/runeconsole-install.log 2>&1
echo "=== runeconsole startup at $(date) ==="

for i in $(seq 1 30); do
  apt-get update -q && apt-get install -y ca-certificates curl openssl && break
  echo "apt retry $i..." && sleep 10
done

cat > /etc/profile.d/runeconsole-installer-env.sh <<'ENVFILE'
export RUNECONSOLE_TEAM_NAME='${team_name}'
export RUNECONSOLE_RUNESPACE_ENDPOINT='${runespace_endpoint}'
export RUNECONSOLE_RUNESPACE_TOKEN='${runespace_token}'
ENVFILE
chmod 600 /etc/profile.d/runeconsole-installer-env.sh
set -a; . /etc/profile.d/runeconsole-installer-env.sh; set +a

INSTALL_URL="https://raw.githubusercontent.com/CryptoLabInc/rune-console/${runeconsole_version}/install.sh"
for i in 1 2 3 4 5; do
  curl -fsSL --retry 5 --retry-delay 10 --connect-timeout 15 "$${INSTALL_URL}" -o /tmp/install.sh && break
  sleep $((i*10))
done

bash /tmp/install.sh --target local --non-interactive --version "${runeconsole_version}"

usermod -aG runeconsole ubuntu

rm -f /etc/profile.d/runeconsole-installer-env.sh
echo "=== completed at $(date) ==="
