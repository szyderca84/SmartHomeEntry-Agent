#!/usr/bin/env bash
# uninstall.sh — SmartHomeEntry Agent uninstaller
# Stops the service, removes the binary, credentials, and SSH keys.
set -euo pipefail

# ---------------------------------------------------------------------------
# Constants (must match install.sh)
# ---------------------------------------------------------------------------
BINARY_NAME="smarthomeentry-agent"
INSTALL_BIN="/usr/local/bin/${BINARY_NAME}"
CONFIG_DIR="/etc/smarthomeentry"
LOG_FILE="/var/log/smarthomeentry.log"
LOCK_FILE="/var/run/smarthomeentry-agent.pid"
SERVICE_NAME="${BINARY_NAME}"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
info()  { echo "[uninstall] $*"; }
die()   { echo "[uninstall] ERROR: $*" >&2; exit 1; }

# ---------------------------------------------------------------------------
# Preflight
# ---------------------------------------------------------------------------
[[ $EUID -eq 0 ]] || die "This script must be run as root (use sudo)."

echo "=== SmartHomeEntry Agent Uninstaller ==="
echo ""
echo "  This will:"
echo "    • Stop and disable the ${SERVICE_NAME} service"
echo "    • Remove the binary from ${INSTALL_BIN}"
echo "    • Remove all credentials and SSH keys from ${CONFIG_DIR}"
echo ""
read -rp "  Continue? [y/N] " CONFIRM
[[ "${CONFIRM,,}" == "y" ]] || { echo "Aborted."; exit 0; }
echo ""

# ---------------------------------------------------------------------------
# Stop and disable service
# ---------------------------------------------------------------------------
if systemctl is-active --quiet "${SERVICE_NAME}" 2>/dev/null; then
    info "Stopping service..."
    systemctl stop "${SERVICE_NAME}"
fi

if systemctl is-enabled --quiet "${SERVICE_NAME}" 2>/dev/null; then
    info "Disabling service..."
    systemctl disable "${SERVICE_NAME}"
fi

if [[ -f "${SERVICE_FILE}" ]]; then
    rm -f "${SERVICE_FILE}"
    info "Removed ${SERVICE_FILE}"
    systemctl daemon-reload
fi

# ---------------------------------------------------------------------------
# Remove binary
# ---------------------------------------------------------------------------
if [[ -f "${INSTALL_BIN}" ]]; then
    rm -f "${INSTALL_BIN}"
    info "Removed ${INSTALL_BIN}"
fi

# ---------------------------------------------------------------------------
# Remove credentials and SSH keys (contains private key — remove securely)
# ---------------------------------------------------------------------------
if [[ -d "${CONFIG_DIR}" ]]; then
    # Overwrite key files before removal to reduce recovery risk.
    for keyfile in "${CONFIG_DIR}"/agent_key "${CONFIG_DIR}"/known_hosts; do
        if [[ -f "${keyfile}" ]]; then
            shred -u "${keyfile}" 2>/dev/null || rm -f "${keyfile}"
        fi
    done
    rm -rf "${CONFIG_DIR}"
    info "Removed ${CONFIG_DIR}"
fi

# ---------------------------------------------------------------------------
# Remove lock file
# ---------------------------------------------------------------------------
[[ -f "${LOCK_FILE}" ]] && { rm -f "${LOCK_FILE}"; info "Removed ${LOCK_FILE}"; }

# ---------------------------------------------------------------------------
# Optionally remove log file
# ---------------------------------------------------------------------------
if [[ -f "${LOG_FILE}" ]]; then
    read -rp "  Remove log file ${LOG_FILE}? [y/N] " RM_LOG
    if [[ "${RM_LOG,,}" == "y" ]]; then
        rm -f "${LOG_FILE}"
        info "Removed ${LOG_FILE}"
    fi
fi

echo ""
echo "=== Uninstall complete ==="
