#!/usr/bin/env bash
# install.sh — SmartHomeEntry Agent installer
# Run as root on the target device (Raspberry Pi / home server).
set -euo pipefail

# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------
BINARY_NAME="smarthomeentry-agent"
INSTALL_BIN="/usr/local/bin/${BINARY_NAME}"
CONFIG_DIR="/etc/smarthomeentry"
ENV_FILE="${CONFIG_DIR}/agent.env"
LOG_FILE="/var/log/smarthomeentry.log"
SERVICE_NAME="${BINARY_NAME}"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
info()  { echo "[install] $*"; }
die()   { echo "[install] ERROR: $*" >&2; exit 1; }

# ---------------------------------------------------------------------------
# Preflight
# ---------------------------------------------------------------------------
[[ $EUID -eq 0 ]] || die "This script must be run as root (use sudo)."

command -v systemctl &>/dev/null || die "systemd is required."

# ---------------------------------------------------------------------------
# Gather credentials interactively
# ---------------------------------------------------------------------------
echo "=== SmartHomeEntry Agent Installer ==="
echo ""

read -rp "  API URL (e.g. https://api.smarthomeentry.example.com): " API_URL
[[ "${API_URL}" == https://* ]] || die "API_URL must start with https://"

read -rp "  Install Token: " INSTALL_TOKEN
[[ -n "${INSTALL_TOKEN}" ]] || die "Install token cannot be empty."

echo ""

# ---------------------------------------------------------------------------
# Build binary if not already built
# ---------------------------------------------------------------------------
BINARY_SRC="${REPO_ROOT}/build/${BINARY_NAME}"

if [[ ! -f "${BINARY_SRC}" ]]; then
    info "Binary not found — building now..."
    command -v go &>/dev/null || die "Go toolchain not found. Install Go 1.22+ or pre-build the binary."
    (cd "${REPO_ROOT}" && make build)
fi

[[ -f "${BINARY_SRC}" ]] || die "Build failed: ${BINARY_SRC} not found."

# ---------------------------------------------------------------------------
# Install binary
# ---------------------------------------------------------------------------
info "Installing binary → ${INSTALL_BIN}"
install -o root -g root -m 755 "${BINARY_SRC}" "${INSTALL_BIN}"

# ---------------------------------------------------------------------------
# Create config directory and write credentials
# ---------------------------------------------------------------------------
info "Creating config directory ${CONFIG_DIR}"
mkdir -p "${CONFIG_DIR}"
chmod 750 "${CONFIG_DIR}"

info "Writing credentials → ${ENV_FILE}"
# Write atomically via a temp file so the secrets are never partially readable.
TMP_ENV="$(mktemp "${CONFIG_DIR}/.agent.env.XXXXXX")"
cat > "${TMP_ENV}" <<EOF
# SmartHomeEntry Agent — environment file
# Managed by install.sh — do not edit manually while service is running.
SMARTHOMEENTRY_API_URL=${API_URL}
SMARTHOMEENTRY_INSTALL_TOKEN=${INSTALL_TOKEN}
EOF
chmod 600 "${TMP_ENV}"
mv "${TMP_ENV}" "${ENV_FILE}"

# ---------------------------------------------------------------------------
# Create log file
# ---------------------------------------------------------------------------
info "Creating log file ${LOG_FILE}"
touch "${LOG_FILE}"
chmod 644 "${LOG_FILE}"

# ---------------------------------------------------------------------------
# Install systemd unit
# ---------------------------------------------------------------------------
info "Installing systemd service → ${SERVICE_FILE}"
install -o root -g root -m 644 "${REPO_ROOT}/systemd/${SERVICE_NAME}.service" "${SERVICE_FILE}"

# ---------------------------------------------------------------------------
# Enable and start
# ---------------------------------------------------------------------------
info "Enabling and starting service"
systemctl daemon-reload
systemctl enable "${SERVICE_NAME}"
systemctl restart "${SERVICE_NAME}"

echo ""
echo "=== Installation complete ==="
echo ""
echo "  Status:  systemctl status ${SERVICE_NAME}"
echo "  Logs:    journalctl -u ${SERVICE_NAME} -f"
echo "           tail -f ${LOG_FILE}"
echo ""
