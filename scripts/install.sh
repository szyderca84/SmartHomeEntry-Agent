#!/bin/sh
# SmartHomeEntry Agent – installer
# Pobiera gotowy pakiet .deb i instaluje agenta.
#
# Użycie (argumenty):
#   sudo sh install.sh --token=xxx --local-addr=localhost:80
#
# Użycie (env):
#   sudo SMARTHOMEENTRY_INSTALL_TOKEN=xxx SMARTHOMEENTRY_LOCAL_ADDR=localhost:80 sh install.sh
#
# Użycie interaktywne (skrypt dopyta o token i adres lokalny):
#   sudo sh install.sh
set -eu

REPO="szyderca84/SmartHomeEntry-Agent"

# ── Env vars (mogą być wbudowane przez panel SmartHomeEntry) ──────────
API_URL="${SMARTHOMEENTRY_API_URL:-https://api.smarthomeentry.com}"
TOKEN="${SMARTHOMEENTRY_INSTALL_TOKEN:-}"
# Celowo BEZ wartości domyślnej. Cichy default (localhost:8080) powodował
# instalacje, w których tunel wstawał poprawnie, ale kierował na port, gdzie
# nic nie nasłuchuje - user widział 502 i nie miał jak zgadnąć dlaczego.
LOCAL_ADDR="${SMARTHOMEENTRY_LOCAL_ADDR:-}"

usage() {
  cat << 'USAGE'
SmartHomeEntry Agent – installer

  --token=<token>          Token instalacyjny z panelu (TTL 15 min)
  --local-addr=<host:port> Adres, pod którym nasłuchuje Twój serwis
  --api-url=<url>          Adres control plane (domyślnie api.smarthomeentry.com)
  -h, --help               Ta pomoc

Typowe adresy lokalne:
  Home Assistant  localhost:8123      Nextcloud   localhost:80 (lub :443)
  Domoticz        localhost:8080      Jellyfin    localhost:8096
  Node-RED        localhost:1880      OpenHAB     localhost:8443
  Grafana         localhost:3000      Portainer   localhost:9000
USAGE
}

# ── Argumenty (mają pierwszeństwo przed env) ──────────────────────────
for arg in "$@"; do
  case "${arg}" in
    --token=*)      TOKEN="${arg#--token=}" ;;
    --local-addr=*) LOCAL_ADDR="${arg#--local-addr=}" ;;
    --api-url=*)    API_URL="${arg#--api-url=}" ;;
    -h|--help)      usage; exit 0 ;;
    *) echo "Nieznany argument: ${arg}"; echo ""; usage; exit 1 ;;
  esac
done

# ── Sprawdź root ──────────────────────────────────────────────────────
[ "$(id -u)" -eq 0 ] || { echo "Uruchom jako root: sudo sh $0"; exit 1; }

# ── Sprawdź systemd ───────────────────────────────────────────────────
command -v systemctl > /dev/null 2>&1 || { echo "Wymagany systemd. Użyj Docker zamiast tego instalatora."; exit 1; }

echo "=== SmartHomeEntry Agent Installer ==="
echo ""

# ── Wejście interaktywne ──────────────────────────────────────────────
# Gdy skrypt leci przez potok (curl ... | sh), stdin to treść skryptu,
# nie klawiatura - czytamy więc z /dev/tty. Brak /dev/tty = tryb
# nieinteraktywny (CI, provisioning) i wtedy wymagamy argumentów.
# Realna próba otwarcia. Samo [ -r /dev/tty ] to test uprawnień i przechodzi
# także tam, gdzie procesu nie ma terminala kontrolującego (systemd, cron).
# Podpowłoka jest tu istotna: nieudane przekierowanie w zwykłej grupie poleceń
# ubija cały skrypt (dash, POSIX), zamiast tylko zwrócić niezerowy status.
have_tty() { ( : < /dev/tty ) 2> /dev/null; }

ask() { # ask <prompt> -> odpowiedź na stdout
  _ans=""
  printf "%s" "$1" > /dev/tty
  IFS= read -r _ans < /dev/tty || _ans=""
  printf "%s" "${_ans}"
}

# host:port - port obowiązkowy, agent dzwoni po TCP i bez portu nie wie dokąd
valid_addr() {
  echo "$1" | grep -Eq '^[A-Za-z0-9._-]+:[0-9]{1,5}$'
}

if [ -z "${TOKEN}" ]; then
  have_tty || { echo "Brak tokenu. Podaj --token=<token> albo SMARTHOMEENTRY_INSTALL_TOKEN."; exit 1; }
  TOKEN=$(ask "  Token instalacyjny: ")
  [ -n "${TOKEN}" ] || { echo "Token nie może być pusty."; exit 1; }
fi

if [ -z "${LOCAL_ADDR}" ]; then
  if ! have_tty; then
    echo "Brak adresu lokalnego. Podaj --local-addr=<host:port> albo SMARTHOMEENTRY_LOCAL_ADDR."
    echo ""
    usage
    exit 1
  fi
  echo "  Pod jakim adresem lokalnym nasłuchuje serwis, który chcesz wystawić?"
  echo ""
  echo "    Home Assistant  localhost:8123      Nextcloud   localhost:80 (lub :443)"
  echo "    Domoticz        localhost:8080      Jellyfin    localhost:8096"
  echo "    Node-RED        localhost:1880      OpenHAB     localhost:8443"
  echo "    Grafana         localhost:3000      Portainer   localhost:9000"
  echo ""
  echo "  Nie wiesz? Sprawdź w drugim terminalu:  sudo ss -tlnp | grep LISTEN"
  echo ""
  LOCAL_ADDR=$(ask "  Adres lokalny (host:port): ")
fi

if ! valid_addr "${LOCAL_ADDR}"; then
  echo "Nieprawidłowy adres lokalny: '${LOCAL_ADDR}'"
  echo "Oczekiwany format host:port, np. localhost:80 albo 192.168.1.50:8123"
  exit 1
fi

# ── Ostrzeż, jeśli pod tym adresem nic nie nasłuchuje ─────────────────
# Nie przerywamy: serwis może wstać później (kolejność bootu, kontener).
# Ale user musi to zobaczyć TERAZ, a nie zgadywać z 502 na subdomenie.
if command -v ss > /dev/null 2>&1; then
  _port="${LOCAL_ADDR##*:}"
  if ! ss -ltn 2>/dev/null | grep -qE "[:.]${_port}[[:space:]]"; then
    echo ""
    echo "  UWAGA: na porcie ${_port} nic teraz nie nasłuchuje."
    echo "  Jeśli serwis dopiero wstanie - w porządku. Jeśli nie, subdomena zwróci 502."
    echo "  Adres zmienisz później w /etc/smarthomeentry/agent.env"
    echo ""
  fi
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

# ── Pobierz numer najnowszej wersji ──────────────────────────────────
LATEST=$(curl -sSf "https://api.github.com/repos/${REPO}/releases/latest" \
  | grep '"tag_name"' | head -1 | sed 's/.*"v\([^"]*\)".*/\1/')

[ -n "${LATEST}" ] || { echo "Nie można pobrać wersji z GitHub."; exit 1; }
echo "  Wersja:       ${LATEST}"

# ── Pobierz i zainstaluj .deb ─────────────────────────────────────────
DEB_URL="https://github.com/${REPO}/releases/download/v${LATEST}/smarthomeentry-agent_${LATEST}_${DEB_ARCH}.deb"
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

echo ""
echo "Agent kieruje ruch na ${LOCAL_ADDR}."
echo "Zmiana adresu:  sudo nano /etc/smarthomeentry/agent.env"
echo "                sudo systemctl restart smarthomeentry-agent"
echo "Logi:           journalctl -u smarthomeentry-agent -f"
