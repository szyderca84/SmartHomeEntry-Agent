# SmartHomeEntry Agent

Encrypted SSH reverse tunnel agent for remote access to Home Assistant / Domoticz behind NAT — no port forwarding, no VPN.

## How it works

The agent opens an outbound SSH connection to a relay server and sets up a reverse tunnel. Users connect to HA/Domoticz through the relay. All traffic is encrypted.

```
HA/Domoticz ──SSH──▶ relay ◀── user browser
```

## Install (Linux / Raspberry Pi)

```sh
curl -sSL https://raw.githubusercontent.com/szyderca84/SmartHomeEntry-Agent/main/scripts/install.sh | sudo sh
```

The script will ask for an install token from the SmartHomeEntry panel.

Non-interactive:

```sh
sudo SMARTHOMEENTRY_INSTALL_TOKEN=xxx sh install.sh
```

## Home Assistant Addon

Add the repository in HA → Supervisor → Add-on Store → ⋮ → Repositories:

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

## Configuration

Environment variables (set automatically by the installer):

| Variable | Description | Default |
|---|---|---|
| `SMARTHOMEENTRY_API_URL` | Control plane URL | `https://api.smarthomeentry.com` |
| `SMARTHOMEENTRY_INSTALL_TOKEN` | Token from the panel | — |
| `SMARTHOMEENTRY_LOCAL_ADDR` | Local server address | `localhost:8080` |

After install the agent runs as a systemd service (`smarthomeentry-agent.service`).

## Uninstall

```sh
sudo sh /usr/local/share/smarthomeentry/uninstall.sh
```

## Build from source

```sh
make build          # binary in build/
make build-all      # amd64 + arm64 + armhf
```
