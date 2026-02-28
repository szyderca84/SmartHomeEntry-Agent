#!/bin/sh
set -eu

# If token is provided via env (e.g. from install.sh), write config automatically.
if [ -n "${SMARTHOMEENTRY_INSTALL_TOKEN:-}" ]; then
  cat > /etc/smarthomeentry/agent.env << EOF
SMARTHOMEENTRY_API_URL=${SMARTHOMEENTRY_API_URL:-https://api.smarthomeentry.com}
SMARTHOMEENTRY_INSTALL_TOKEN=${SMARTHOMEENTRY_INSTALL_TOKEN}
SMARTHOMEENTRY_LOCAL_ADDR=${SMARTHOMEENTRY_LOCAL_ADDR:-localhost:8080}
EOF
  chmod 600 /etc/smarthomeentry/agent.env
fi

systemctl daemon-reload
systemctl enable smarthomeentry-agent

if [ -f /etc/smarthomeentry/agent.env ]; then
  systemctl restart smarthomeentry-agent
  echo ""
  echo "=== SmartHomeEntry Agent uruchomiony ==="
  echo "Status: systemctl status smarthomeentry-agent"
  echo "Logi:   journalctl -u smarthomeentry-agent -f"
else
  echo ""
  echo "=== SmartHomeEntry Agent zainstalowany ==="
  echo "Skonfiguruj token w pliku /etc/smarthomeentry/agent.env:"
  echo ""
  echo "  SMARTHOMEENTRY_API_URL=https://api.smarthomeentry.com"
  echo "  SMARTHOMEENTRY_INSTALL_TOKEN=<twój_token>"
  echo "  SMARTHOMEENTRY_LOCAL_ADDR=localhost:8080"
  echo ""
  echo "Następnie uruchom:"
  echo "  sudo systemctl start smarthomeentry-agent"
fi
