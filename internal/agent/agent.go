package agent

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/smarthomeentry/agent/internal/api"
	"github.com/smarthomeentry/agent/internal/backoff"
	"github.com/smarthomeentry/agent/internal/metrics"
	"github.com/smarthomeentry/agent/internal/tunnel"
)

const (
	keyFilePath          = "/etc/smarthomeentry/agent_key"
	configDir            = "/etc/smarthomeentry"
	defaultLocalAddr     = "localhost:8080"
	inactivePollInterval = 5 * time.Minute
	stableThreshold      = time.Minute
)

// ErrTokenRevoked signals that the control plane rejected our token during
// periodic re-validation (HTTP 401/403). The agent should stop gracefully.
var ErrTokenRevoked = fmt.Errorf("install token revoked by control plane")

// ErrLocalTargetChanged sygnalizuje, ze panel wskazal inny adres lokalny.
// Nie jest to awaria: zrywamy tunel, zeby cykl wystartowal od nowa i podniosl
// nowa wartosc. Bez tego zmiana adresu w panelu wymagalaby wejscia userowi na
// maszyne i restartu uslugi (awaria 2026-09-07).
var ErrLocalTargetChanged = fmt.Errorf("local target changed in control plane")

// validLocalAddr - host:port. Wartosc przychodzi z sieci i trafia prosto do
// net.Dial, wiec nie ufamy jej na slowo; przy odrzuceniu zostaje adres z env.
func validLocalAddr(addr string) bool {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || host == "" {
		return false
	}
	n, err := strconv.Atoi(port)
	return err == nil && n >= 1 && n <= 65535
}

type Agent struct {
	api       *api.Client
	bo        *backoff.Backoff
	lockFH    *os.File
	localAddr string
}

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

func (a *Agent) Close() {
	if a.lockFH != nil {
		a.lockFH.Close()
		_ = os.Remove(lockFilePath)
	}
}

// Run is the main blocking loop. Returns nil on clean shutdown (ctx cancelled)
// and a non-nil error only for unrecoverable failures (e.g. invalid token).
func (a *Agent) Run(ctx context.Context) error {
	log.Println("SmartHomeEntry Agent starting")

	if err := a.api.ValidateToken(ctx); err != nil {
		return fmt.Errorf("install token validation failed: %w", err)
	}
	log.Println("install token validated")

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		err := a.runCycle(ctx)

		if err == nil || errors.Is(err, context.Canceled) {
			return ctx.Err()
		}

		if errors.Is(err, ErrLocalTargetChanged) {
			log.Println("reconnecting with the new local target")
			if !sleepCtx(ctx, 2*time.Second) {
				return ctx.Err()
			}
			continue
		}

		if errors.Is(err, tunnel.ErrInactive) {
			log.Printf("agent is inactive — retrying config in %s", inactivePollInterval)
			if !sleepCtx(ctx, inactivePollInterval) {
				return ctx.Err()
			}
			continue
		}

		wait := a.bo.Next()
		log.Printf("cycle error: %v — reconnecting in %s", err, wait.Truncate(time.Millisecond))
		if !sleepCtx(ctx, wait) {
			return ctx.Err()
		}
	}
}

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

	// Panel ma pierwszenstwo nad agent.env - dzieki temu zly adres da sie
	// poprawic zdalnie. Pusta lub niepoprawna wartosc = zostajemy przy env.
	effectiveAddr := a.localAddr
	if cfg.LocalAddr != "" {
		if validLocalAddr(cfg.LocalAddr) {
			if cfg.LocalAddr != effectiveAddr {
				log.Printf("local target from control plane: %s (agent.env has %s)", cfg.LocalAddr, a.localAddr)
			}
			effectiveAddr = cfg.LocalAddr
		} else {
			log.Printf("WARNING: control plane sent an invalid local address %q — keeping %s", cfg.LocalAddr, effectiveAddr)
		}
	}

	checkDomoticz(effectiveAddr)

	// Use key from config if provided, otherwise fall back to key on disk
	// (server returns empty string after the token has been consumed).
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

	var hbCount int
	err = tunnel.Run(ctx, &tunnel.Config{
		Host:       cfg.Host,
		Port:       cfg.Port,
		TunnelPort: cfg.TunnelPort,
		SSHUser:    cfg.SSHUser,
		PrivateKey: privateKey,
		LocalAddr:  effectiveAddr,
		HeartbeatFunc: func(hbCtx context.Context) (bool, error) {
			hbCount++

			// Re-validate token every 10 heartbeat cycles (~10 minutes).
			if hbCount%10 == 0 {
				if vErr := a.api.ValidateToken(hbCtx); vErr != nil {
					if errors.Is(vErr, api.ErrUnauthorized) {
						log.Println("token re-validation failed: 401 unauthorized — stopping tunnel")
						return false, ErrTokenRevoked
					}
					log.Printf("token re-validation error (non-fatal): %v", vErr)
				} else {
					log.Println("token re-validation OK")
				}
			}

			var m *api.HeartbeatMetrics
			if s, mErr := metrics.Collect(hbCtx); mErr != nil {
				log.Printf("metrics collection error: %v (skipping metrics this heartbeat)", mErr)
			} else {
				m = &api.HeartbeatMetrics{
					CPUPercent: s.CPUPercent,
					RAMPercent: s.RAMPercent,
					RAMUsedMB:  s.RAMUsedMB,
					RAMTotalMB: s.RAMTotalMB,
				}
				log.Printf("metrics: cpu=%.1f%% ram=%.1f%% (%d/%d MB)",
					m.CPUPercent, m.RAMPercent, m.RAMUsedMB, m.RAMTotalMB)
			}

			resp, hbErr := a.api.SendHeartbeat(hbCtx, cfg.HeartbeatURL, m)
			if hbErr != nil {
				return true, hbErr
			}

			// Zmiana adresu w panelu wchodzi w zycie w ciagu jednego cyklu
			// heartbeatu, bez restartu uslugi po stronie uzytkownika.
			if resp.LocalAddr != "" && resp.LocalAddr != effectiveAddr && validLocalAddr(resp.LocalAddr) {
				log.Printf("local target changed: %s -> %s — reconnecting", effectiveAddr, resp.LocalAddr)
				return false, ErrLocalTargetChanged
			}
			return resp.Active, nil
		},
	})

	if elapsed := time.Since(start); elapsed >= stableThreshold {
		log.Printf("connection was stable for %s — resetting backoff", elapsed.Truncate(time.Second))
		a.bo.Reset()
	}

	return err
}

func checkDomoticz(addr string) {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		log.Printf("WARNING: local server not reachable at %s: %v", addr, err)
		return
	}
	conn.Close()
	log.Printf("local server reachable at %s", addr)
}

func writeKey(key string) error {
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	if err := os.WriteFile(keyFilePath, []byte(key), 0o600); err != nil {
		return fmt.Errorf("write key to %s: %w", keyFilePath, err)
	}
	return nil
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}
