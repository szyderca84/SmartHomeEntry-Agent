# SmartHomeEntry Agent

Agent zestawiający szyfrowany tunel SSH do zdalnego dostępu do Home Assistant / Domoticz za NAT-em — bez otwierania portów, bez VPN.

## Jak działa

Agent łączy się wychodzącym połączeniem SSH z serwerem relay i tworzy reverse tunnel. Użytkownik łączy się z HA/Domoticz przez relay, cały ruch idzie przez szyfrowane połączenie SSH.

```
HA/Domoticz ──SSH──▶ relay ◀── przeglądarka użytkownika
```

## Instalacja (Linux / Raspberry Pi)

```sh
curl -sSL https://raw.githubusercontent.com/szyderca84/SmartHomeEntry-Agent/main/scripts/install.sh | sudo sh
```

Skrypt zapyta o token instalacyjny z panelu SmartHomeEntry.

Lub z tokenem w env (np. w skrypcie):

```sh
sudo SMARTHOMEENTRY_INSTALL_TOKEN=xxx sh install.sh
```

## Home Assistant Addon

Dodaj repozytorium w HA → Supervisor → Add-on Store → trzy kropki → Repositories:

```
https://github.com/szyderca84/SmartHomeEntry-Agent
```

## Docker

```sh
docker run -d \
  -e SMARTHOMEENTRY_API_URL=https://api.smarthomeentry.com \
  -e SMARTHOMEENTRY_INSTALL_TOKEN=xxx \
  ghcr.io/szyderca84/smarthomeentry-agent:latest
```

## Konfiguracja

Zmienne środowiskowe (ustawiane automatycznie przez installer):

| Zmienna | Opis | Domyślnie |
|---|---|---|
| `SMARTHOMEENTRY_API_URL` | URL panelu | `https://api.smarthomeentry.com` |
| `SMARTHOMEENTRY_INSTALL_TOKEN` | Token z panelu | — |
| `SMARTHOMEENTRY_LOCAL_ADDR` | Adres lokalnego serwera | `localhost:8080` |

Po instalacji agent działa jako usługa systemd (`smarthomeentry-agent.service`).

## Odinstalowanie

```sh
sudo sh /usr/local/share/smarthomeentry/uninstall.sh
```

## Budowanie ze źródeł

```sh
make build          # binarka w build/
make build-all      # amd64 + arm64 + armhf
```
