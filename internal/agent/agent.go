// Package agent implements the SmartHomeEntry agent state machine.
//
// Main loop:
//  1. Validate install token (once at startup)
//  2. Fetch config from control plane
//  3. If active=false → wait 5 m, repeat from 2
//  4. Check Domoticz reachability (warn only)
//  5. Write SSH private key to disk (0600)
//  6. Run reverse SSH tunnel (blocks until disconnect or deactivation)
//  7. On disconnect → exponential backoff → repeat from 2
//  8. On deactivation (active=false from heartbeat) → wait 5 m → repeat from 2
package agent

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"github.com/smarthomeentry/agent/internal/api"
	"github.com/smarthomeentry/agent/internal/backoff"
	"github.com/smarthomeentry/agent/internal/tunnel"
)

const (
	// keyFilePath stores the SSH private key returned by the control plane.
	keyFilePath = "/etc/smarthomeentry/agent_key"
	// configDir is created if absent.
	configDir = "/etc/smarthomeentry"

	// defaultLocalAddr is used when SMARTHOMEENTRY_LOCAL_ADDR is not set.
	defaultLocalAddr = "localhost:8080"

	// inactivePollInterval is how long to wait before re-fetching config
	// when the control plane signals active=false.
	inactivePollInterval = 5 * time.Minute

	// stableThreshold: if a connection lasts this long we treat it as healthy
	// and reset the backoff counter on the next disconnect.
	stableThreshold = time.Minute
)

// Agent is the top-level orchestrator. Create with New; run with Run.
type Agent struct {
	api       *api.Client
	bo        *backoff.Backoff
	lockFH    *os.File
	localAddr string
}

// New creates an Agent, validates inputs, and acquires the process-level lock
// (preventing multiple instances). The caller must defer a.Close().
func New(apiURL, token, localAddr string) (*Agent, error) {
	client, err := api.New(apiURL, token)
	if err != nil {
		return nil, fmt.Errorf("api client: %w", err)
	}

	lockFH, err := acquireLock()
	if err != nil {
		return nil, err
	}

	if localAddr == "" {
		localAddr = defaultLocalAddr
	}

	return &Agent{
		api:       client,
		bo:        backoff.New(),
		lockFH:    lockFH,
		localAddr: localAddr,
	}, nil
}

// Close releases the process lock and removes the PID file.
func (a *Agent) Close() {
	if a.lockFH != nil {
		a.lockFH.Close()
		_ = os.Remove(lockFilePath)
	}
}

// Run is the main blocking loop. It returns nil on clean shutdown (ctx cancelled)
// and a non-nil error only for unrecoverable failures (e.g. invalid token).
func (a *Agent) Run(ctx context.Context) error {
	log.Println("SmartHomeEntry Agent starting")

	// Validate the install token once at startup. A bad token is unrecoverable.
	if err := a.api.ValidateToken(ctx); err != nil {
		return fmt.Errorf("install token validation failed: %w", err)
	}
	log.Println("install token validated")

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		err := a.runCycle(ctx)

		// Clean shutdown.
		if err == nil || errors.Is(err, context.Canceled) {
			return ctx.Err()
		}

		// Control plane deactivated the agent: sleep 5 m then re-fetch config.
		if errors.Is(err, tunnel.ErrInactive) {
			log.Printf("agent is inactive — retrying config in %s", inactivePollInterval)
			if !sleepCtx(ctx, inactivePollInterval) {
				return ctx.Err()
			}
			continue
		}

		// Transient error (network, SSH, etc.): exponential backoff.
		wait := a.bo.Next()
		log.Printf("cycle error: %v — reconnecting in %s", err, wait.Truncate(time.Millisecond))
		if !sleepCtx(ctx, wait) {
			return ctx.Err()
		}
	}
}

// runCycle performs one full connect-run-disconnect cycle:
// fetch config → check domoticz → write key → run tunnel.
// Returns ErrInactive, ctx.Err(), or a transient error.
func (a *Agent) runCycle(ctx context.Context) error {
	log.Println("fetching config from control plane")
	cfg, err := a.api.FetchConfig(ctx)
	if err != nil {
		return fmt.Errorf("fetch config: %w", err)
	}
	log.Printf("config: relay=%s ssh_port=%d tunnel_port=%d active=%v",
		cfg.Host, cfg.Port, cfg.TunnelPort, cfg.Active)

	if !cfg.Active {
		return tunnel.ErrInactive
	}

	checkDomoticz(a.localAddr)

	// Resolve the private key: use the one from config (first call) or fall
	// back to the key previously written to disk (subsequent calls after the
	// token has been consumed and the server returns an empty string).
	privateKey := cfg.PrivateKey
	if privateKey != "" {
		if err := writeKey(privateKey); err != nil {
			return fmt.Errorf("write SSH key: %w", err)
		}
	} else {
		keyBytes, err := os.ReadFile(keyFilePath)
		if err != nil {
			return fmt.Errorf("SSH key not in config and not on disk (%s): %w — regenerate install token", keyFilePath, err)
		}
		privateKey = string(keyBytes)
		log.Printf("using SSH key from disk (%s)", keyFilePath)
	}

	start := time.Now()

	err = tunnel.Run(ctx, &tunnel.Config{
		Host:       cfg.Host,
		Port:       cfg.Port,
		TunnelPort: cfg.TunnelPort,
		SSHUser:    cfg.SSHUser,
		PrivateKey: privateKey,
		LocalAddr:  a.localAddr,
		HeartbeatFunc: func(hbCtx context.Context) (bool, error) {
			resp, hbErr := a.api.SendHeartbeat(hbCtx, cfg.HeartbeatURL)
			if hbErr != nil {
				// Transient error: keep tunnel alive, do not deactivate.
				return true, hbErr
			}
			return resp.Active, nil
		},
	})

	// If the connection was stable, reset backoff so the next error starts fresh.
	elapsed := time.Since(start)
	if elapsed >= stableThreshold {
		log.Printf("connection was stable for %s — resetting backoff", elapsed.Truncate(time.Second))
		a.bo.Reset()
	}

	return err
}

// checkDomoticz tests whether the local server is reachable and logs the result.
// It is a warning-only check; the agent continues regardless.
func checkDomoticz(addr string) {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		log.Printf("WARNING: local server not reachable at %s: %v", addr, err)
		return
	}
	conn.Close()
	log.Printf("local server reachable at %s", addr)
}

// writeKey writes the PEM-encoded private key to keyFilePath with mode 0600.
func writeKey(key string) error {
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	if err := os.WriteFile(keyFilePath, []byte(key), 0o600); err != nil {
		return fmt.Errorf("write key to %s: %w", keyFilePath, err)
	}
	return nil
}

// sleepCtx sleeps for d, returning false early if ctx is cancelled.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}
