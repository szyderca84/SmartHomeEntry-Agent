We are implementing SmartHomeEntry Client Agent in Go

This agent must:

- work whether relay is on same VPS or separate VPS
- never assume relay == API host
- use config returned from API
- bind reverse SSH to 127.0.0.1 only
- implement exponential backoff with jitter

I will now provide the full specification.

You are a senior Go systems engineer.

Build SmartHomeEntry Agent.

The agent must work whether relay runs:

- On same VPS as control plane
- On separate VPS

Agent must NEVER assume relay == API host.

It must use values returned from config endpoint.

--------------------------------------------------
CRITICAL RULE
--------------------------------------------------

Reverse SSH must use:

-R 127.0.0.1:{port}:localhost:8080

Never 0.0.0.0.

--------------------------------------------------
FLOW
--------------------------------------------------

1. Validate install token
2. Fetch config from API_URL
3. Receive:
   host (relay host)
   port
   ssh_user
   private_key
   active
   heartbeat_url

4. Check Domoticz reachable
5. Store key (0600)
6. Connect to relay host

--------------------------------------------------
RECONNECT STRATEGY
--------------------------------------------------

Implement exponential backoff with jitter:

- start 2s
- max 5m
- random jitter
- reset after stable connection

Prevent reconnect storm.

--------------------------------------------------
HEARTBEAT
--------------------------------------------------

Send every 60s.
If active=false:
- close tunnel
- retry config every 5m

--------------------------------------------------
SAFETY
--------------------------------------------------

- Lock file
- No multiple instances
- No fork loops
- Log to /var/log/smarthomeentry.log

--------------------------------------------------
INSTALLER
--------------------------------------------------

install.sh:
- install binary
- create systemd
- enable service

uninstall.sh:
- remove service
- remove keys

--------------------------------------------------
OUTPUT:
- full Go project
- backoff logic
- systemd
- explanation
