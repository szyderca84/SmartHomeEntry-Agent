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
	keepAliveInterval = 30 * time.Second
	keepAliveTimeout  = 10 * time.Second
	knownHostsPath    = "/etc/smarthomeentry/known_hosts"
)

var ErrInactive = errors.New("agent deactivated by server")

type Config struct {
	Host          string
	Port          int
	TunnelPort    int
	SSHUser       string
	PrivateKey    string
	HeartbeatFunc func(ctx context.Context) (active bool, err error)
	LocalAddr     string
}

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

	// Always bind to 127.0.0.1 — never 0.0.0.0.
	bindAddr := fmt.Sprintf("127.0.0.1:%d", cfg.TunnelPort)
	listener, err := client.Listen("tcp", bindAddr)
	if err != nil {
		return fmt.Errorf("request reverse forward %s: %w", bindAddr, err)
	}
	defer listener.Close()

	log.Printf("reverse tunnel active: relay %s → %s", bindAddr, localAddr)

	tunnelCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	tunnelErr := make(chan error, 3)

	go func() {
		if err := runKeepalive(tunnelCtx, client); err != nil {
			log.Printf("keepalive error: %v — treating connection as dead", err)
			tunnelErr <- fmt.Errorf("keepalive: %w", err)
		}
	}()

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

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				select {
				case <-tunnelCtx.Done():
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

// buildHostKeyCallback returns a TOFU (Trust On First Use) host key callback
// backed by a known_hosts file.
func buildHostKeyCallback(knownHostsFile string) (ssh.HostKeyCallback, error) {
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
		cb, err := knownhosts.New(knownHostsFile)
		if err != nil {
			return fmt.Errorf("load known_hosts: %w", err)
		}

		kerr := cb(hostname, remote, key)
		if kerr == nil {
			return nil
		}

		var keyErr *knownhosts.KeyError
		if errors.As(kerr, &keyErr) && len(keyErr.Want) > 0 {
			return fmt.Errorf(
				"HOST KEY MISMATCH for %s — possible MITM attack! "+
					"Remove %s to reset if the relay key legitimately changed",
				hostname, knownHostsFile,
			)
		}

		// New host — trust on first use.
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
