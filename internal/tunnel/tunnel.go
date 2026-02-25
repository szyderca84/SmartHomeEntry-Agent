// Package tunnel manages a persistent reverse SSH tunnel from the relay server
// to the local Domoticz instance.
//
// SECURITY INVARIANT: the reverse port forward ALWAYS binds to 127.0.0.1 on
// the relay — never to 0.0.0.0. This prevents public exposure of dynamic ports.
package tunnel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

const (
	// keepAliveInterval is how often SSH keepalive requests are sent to the relay.
	keepAliveInterval = 30 * time.Second

	// keepAliveTimeout is how long we wait for a keepalive response before
	// treating the connection as dead.
	keepAliveTimeout = 10 * time.Second

	// knownHostsPath stores trusted relay host keys (TOFU model).
	knownHostsPath = "/etc/smarthomeentry/known_hosts"
)

// ErrInactive is returned by Run when the control plane signals active=false.
// The agent should stop the tunnel and retry the config poll after 5 minutes.
var ErrInactive = errors.New("agent deactivated by server")

// Config holds all parameters needed to establish and maintain the tunnel.
// It is decoupled from the API package to avoid import cycles.
type Config struct {
	// Host is the DNS name or IP of the relay server.
	// It is NEVER assumed to equal the API host.
	Host string
	// Port is the SSH daemon port on the relay (typically 22 or 2222).
	Port int
	// TunnelPort is the port the relay sshd binds for the reverse forward.
	// Bound to 127.0.0.1 only — NEVER 0.0.0.0.
	TunnelPort int
	// SSHUser is the username for the relay SSH connection.
	SSHUser string
	// PrivateKey is a PEM-encoded private key with no passphrase.
	PrivateKey string
	// HeartbeatFunc is called every 60 seconds. Returning active=false causes
	// Run to close the tunnel and return ErrInactive.
	HeartbeatFunc func(ctx context.Context) (active bool, err error)
	// LocalAddr is the address of the local home automation server to proxy to.
	// Defaults to "localhost:8080" if empty.
	LocalAddr string
}

// Run establishes the reverse SSH tunnel and blocks until one of the following:
//   - ctx is cancelled (returns ctx.Err())
//   - the SSH connection or keepalive fails (returns a wrapped error)
//   - the heartbeat signals active=false (returns ErrInactive)
func Run(ctx context.Context, cfg *Config) error {
	localAddr := cfg.LocalAddr
	if localAddr == "" {
		localAddr = "localhost:8080"
	}

	signer, err := ssh.ParsePrivateKey([]byte(cfg.PrivateKey))
	if err != nil {
		return fmt.Errorf("parse private key: %w", err)
	}

	hkc, err := buildHostKeyCallback(knownHostsPath)
	if err != nil {
		return fmt.Errorf("host key setup: %w", err)
	}

	clientCfg := &ssh.ClientConfig{
		User:            cfg.SSHUser,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: hkc,
		Timeout:         30 * time.Second,
	}

	relayAddr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	log.Printf("connecting to relay %s as user %q", relayAddr, cfg.SSHUser)

	client, err := ssh.Dial("tcp", relayAddr, clientCfg)
	if err != nil {
		return fmt.Errorf("dial relay %s: %w", relayAddr, err)
	}
	defer client.Close()

	// CRITICAL: bind to 127.0.0.1 only — never 0.0.0.0.
	bindAddr := fmt.Sprintf("127.0.0.1:%d", cfg.TunnelPort)
	listener, err := client.Listen("tcp", bindAddr)
	if err != nil {
		return fmt.Errorf("request reverse forward %s: %w", bindAddr, err)
	}
	defer listener.Close()

	log.Printf("reverse tunnel active: relay %s → %s", bindAddr, localAddr)

	// tunnelCtx is cancelled when this function returns, stopping all goroutines.
	tunnelCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Buffered so goroutines can send without blocking even after we've returned.
	tunnelErr := make(chan error, 3)

	// SSH keepalive — detect dead connections before the OS timeout.
	go func() {
		if err := runKeepalive(tunnelCtx, client); err != nil {
			log.Printf("keepalive error: %v — treating connection as dead", err)
			tunnelErr <- fmt.Errorf("keepalive: %w", err)
		}
	}()

	// Heartbeat — inform the control plane we are alive, watch for deactivation.
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-tunnelCtx.Done():
				return
			case <-ticker.C:
				active, err := cfg.HeartbeatFunc(tunnelCtx)
				if err != nil {
					log.Printf("heartbeat error: %v (keeping tunnel alive)", err)
					continue
				}
				if !active {
					log.Println("control plane deactivated agent — closing tunnel")
					tunnelErr <- ErrInactive
					return
				}
				log.Println("heartbeat OK")
			}
		}
	}()

	// Accept reverse connections from the relay and proxy them to Domoticz.
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				select {
				case <-tunnelCtx.Done():
					// Orderly shutdown; listener.Close() triggers this path.
				default:
					tunnelErr <- fmt.Errorf("listener accept: %w", err)
				}
				return
			}
			go proxyConn(conn, localAddr)
		}
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-tunnelErr:
		return err
	}
}

// proxyConn bidirectionally proxies a relay connection to the local home automation server.
func proxyConn(remote net.Conn, localAddr string) {
	defer remote.Close()

	local, err := net.DialTimeout("tcp", localAddr, 5*time.Second)
	if err != nil {
		log.Printf("cannot reach local server at %s: %v", localAddr, err)
		return
	}
	defer local.Close()

	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(local, remote); done <- struct{}{} }()
	go func() { _, _ = io.Copy(remote, local); done <- struct{}{} }()
	<-done
}

// runKeepalive sends periodic SSH keepalive requests to detect dead connections.
// It returns an error as soon as a keepalive fails or times out.
func runKeepalive(ctx context.Context, client *ssh.Client) error {
	ticker := time.NewTicker(keepAliveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			errCh := make(chan error, 1)
			go func() {
				_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
				errCh <- err
			}()
			select {
			case err := <-errCh:
				if err != nil {
					return fmt.Errorf("keepalive request failed: %w", err)
				}
			case <-time.After(keepAliveTimeout):
				return fmt.Errorf("keepalive timed out after %s", keepAliveTimeout)
			case <-ctx.Done():
				return nil
			}
		}
	}
}

// buildHostKeyCallback returns a host key callback using a TOFU (Trust On First Use)
// strategy backed by a known_hosts file at knownHostsFile.
//
//   - First connection to a host: key is stored and trusted.
//   - Subsequent connections: key must match the stored one.
//   - Key mismatch: hard error to prevent MITM attacks.
func buildHostKeyCallback(knownHostsFile string) (ssh.HostKeyCallback, error) {
	// Ensure the directory and file exist.
	if err := os.MkdirAll("/etc/smarthomeentry", 0o755); err != nil {
		return nil, fmt.Errorf("create config dir: %w", err)
	}
	if _, err := os.Stat(knownHostsFile); os.IsNotExist(err) {
		f, err := os.OpenFile(knownHostsFile, os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return nil, fmt.Errorf("create known_hosts: %w", err)
		}
		f.Close()
	}

	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		// Re-read the file on each call so external updates are reflected.
		cb, err := knownhosts.New(knownHostsFile)
		if err != nil {
			return fmt.Errorf("load known_hosts: %w", err)
		}

		kerr := cb(hostname, remote, key)
		if kerr == nil {
			return nil // Host is known and key matches.
		}

		var keyErr *knownhosts.KeyError
		if errors.As(kerr, &keyErr) && len(keyErr.Want) > 0 {
			// Host is known but key differs — potential MITM.
			return fmt.Errorf(
				"HOST KEY MISMATCH for %s — possible MITM attack! "+
					"Remove %s to reset if the relay key legitimately changed",
				hostname, knownHostsFile,
			)
		}

		// Host not yet seen — TOFU: trust and persist.
		log.Printf("[TOFU] Trusting new host key for %s (%s %s)",
			hostname, key.Type(), ssh.FingerprintSHA256(key))

		line := knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key)
		f, err := os.OpenFile(knownHostsFile, os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return fmt.Errorf("save host key to %s: %w", knownHostsFile, err)
		}
		defer f.Close()
		_, err = fmt.Fprintln(f, line)
		return err
	}, nil
}
