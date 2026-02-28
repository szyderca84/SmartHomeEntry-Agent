#!/bin/sh
# SmartHomeEntry Agent – installer
# Pobiera gotowy pakiet .deb i instaluje agenta.
#
# Użycie (nieinteraktywne, token z env):
#   sudo SMARTHOMEENTRY_INSTALL_TOKEN=xxx sh install.sh
#
# Użycie interaktywne (bez tokenu w env):
#   sudo sh install.sh
set -eu

# ── Env vars (mogą być wbudowane przez panel SmartHomeEntry) ──────────
API_URL="${SMARTHOMEENTRY_API_URL:-https://api.smarthomeentry.com}"
TOKEN="${SMARTHOMEENTRY_INSTALL_TOKEN:-}"
LOCAL_ADDR="${SMARTHOMEENTRY_LOCAL_ADDR:-localhost:8080}"

# ── Sprawdź root ──────────────────────────────────────────────────────
[ "$(id -u)" -eq 0 ] || { echo "Uruchom jako root: sudo sh $0"; exit 1; }

# ── Sprawdź systemd ───────────────────────────────────────────────────
command -v systemctl > /dev/null 2>&1 || { echo "Wymagany systemd. Użyj Docker zamiast tego instalatora."; exit 1; }

echo "=== SmartHomeEntry Agent Installer ==="
echo ""

# ── Interaktywne pytania jeśli brak env ───────────────────────────────
if [ -z "${TOKEN}" ]; then
  printf "  Token instalacyjny: "
  read -r TOKEN
  [ -n "${TOKEN}" ] || { echo "Token nie może być pusty."; exit 1; }
fi

echo "  API:   ${API_URL}"
echo "  Addr:  ${LOCAL_ADDR}"
echo ""

# ── Wykryj architekturę ───────────────────────────────────────────────
ARCH=$(dpkg --print-architecture 2>/dev/null || uname -m)
case "${ARCH}" in
  amd64|x86_64)      DEB_ARCH=amd64 ;;
  arm64|aarch64)     DEB_ARCH=arm64 ;;
  armhf|armv7l|arm)  DEB_ARCH=armhf ;;
  *) echo "Nieobsługiwana architektura: ${ARCH}"; exit 1 ;;
esac

echo "  Architektura: ${DEB_ARCH}"

# ── Pobierz i zainstaluj .deb przez API SmartHomeEntry ───────────────
DEB_URL="${API_URL}/api/agent/download?arch=${DEB_ARCH}"
TMP_DEB=$(mktemp /tmp/smarthomeentry-XXXXXX.deb)

echo ""
echo "Pobieranie pakietu..."
curl -sSfL "${DEB_URL}" -o "${TMP_DEB}" || {
  echo "Nie można pobrać pakietu: ${DEB_URL}"
  rm -f "${TMP_DEB}"
  exit 1
}

echo "Instalowanie..."
SMARTHOMEENTRY_API_URL="${API_URL}" \
SMARTHOMEENTRY_INSTALL_TOKEN="${TOKEN}" \
SMARTHOMEENTRY_LOCAL_ADDR="${LOCAL_ADDR}" \
  dpkg -i "${TMP_DEB}"

rm -f "${TMP_DEB}"
