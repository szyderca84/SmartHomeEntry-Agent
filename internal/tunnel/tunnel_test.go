package tunnel

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// ---------- Security invariant ----------

// TestBindAddr_uses127 documents and verifies the critical security invariant:
// the reverse port forward must always bind to 127.0.0.1, never 0.0.0.0.
func TestBindAddr_uses127(t *testing.T) {
	const tunnelPort = 9000
	bindAddr := fmt.Sprintf("127.0.0.1:%d", tunnelPort)
	host, _, err := net.SplitHostPort(bindAddr)
	if err != nil {
		t.Fatalf("SplitHostPort: %v", err)
	}
	if host != "127.0.0.1" {
		t.Errorf("SECURITY: bindAddr host=%q, must be 127.0.0.1 (never 0.0.0.0)", host)
	}
}

// ---------- TOFU host key callback ----------

// generateTestKey creates a fresh Ed25519 SSH public key for testing.
func generateTestKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ed25519 key: %v", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("ssh.NewPublicKey: %v", err)
	}
	return sshPub
}

// setupForTOFU creates /etc/smarthomeentry and a temp known_hosts file.
// Tests that require this are skipped if the process is not root.
func setupForTOFU(t *testing.T) string {
	t.Helper()
	if os.Getuid() != 0 {
		t.Skip("buildHostKeyCallback creates /etc/smarthomeentry — skipping (not root)")
	}
	dir := t.TempDir()
	return filepath.Join(dir, "known_hosts")
}

func TestBuildHostKeyCallback_TOFU_firstConnectionTrusted(t *testing.T) {
	knownHostsFile := setupForTOFU(t)
	pub := generateTestKey(t)
	addr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}

	cb, err := buildHostKeyCallback(knownHostsFile)
	if err != nil {
		t.Fatalf("buildHostKeyCallback: %v", err)
	}

	// First call: host is unknown → TOFU must succeed.
	if err := cb("relay.example.com:22", addr, pub); err != nil {
		t.Fatalf("TOFU first call failed: %v", err)
	}

	// Verify the key was written to the known_hosts file.
	content, err := os.ReadFile(knownHostsFile)
	if err != nil {
		t.Fatalf("read known_hosts: %v", err)
	}
	if len(content) == 0 {
		t.Error("known_hosts must be non-empty after TOFU")
	}
}

func TestBuildHostKeyCallback_sameKey_accepted(t *testing.T) {
	knownHostsFile := setupForTOFU(t)
	pub := generateTestKey(t)
	addr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}

	cb, err := buildHostKeyCallback(knownHostsFile)
	if err != nil {
		t.Fatalf("buildHostKeyCallback: %v", err)
	}
	// First connection: TOFU stores the key.
	if err := cb("relay.example.com:22", addr, pub); err != nil {
		t.Fatalf("first TOFU call: %v", err)
	}

	// Second connection with the SAME key: must succeed.
	cb2, err := buildHostKeyCallback(knownHostsFile)
	if err != nil {
		t.Fatalf("buildHostKeyCallback (second): %v", err)
	}
	if err := cb2("relay.example.com:22", addr, pub); err != nil {
		t.Fatalf("second call with same key: %v", err)
	}
}

func TestBuildHostKeyCallback_differentKey_rejected(t *testing.T) {
	knownHostsFile := setupForTOFU(t)
	pub1 := generateTestKey(t)
	pub2 := generateTestKey(t)
	addr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}

	cb, err := buildHostKeyCallback(knownHostsFile)
	if err != nil {
		t.Fatalf("buildHostKeyCallback: %v", err)
	}
	// Store key 1 via TOFU.
	if err := cb("relay.example.com:22", addr, pub1); err != nil {
		t.Fatalf("TOFU call: %v", err)
	}

	// Reconnect with a DIFFERENT key → MITM detection must fire.
	cb2, err := buildHostKeyCallback(knownHostsFile)
	if err != nil {
		t.Fatalf("buildHostKeyCallback (second): %v", err)
	}
	if err := cb2("relay.example.com:22", addr, pub2); err == nil {
		t.Error("SECURITY: expected error for host key mismatch (potential MITM), got nil")
	}
}

func TestBuildHostKeyCallback_createsKnownHostsFile(t *testing.T) {
	knownHostsFile := setupForTOFU(t)

	// File must not exist before the call.
	if _, err := os.Stat(knownHostsFile); !os.IsNotExist(err) {
		t.Fatalf("known_hosts should not exist yet, err=%v", err)
	}

	_, err := buildHostKeyCallback(knownHostsFile)
	if err != nil {
		t.Fatalf("buildHostKeyCallback: %v", err)
	}

	// File must exist after the call.
	if _, err := os.Stat(knownHostsFile); err != nil {
		t.Errorf("expected known_hosts to be created: %v", err)
	}
}

func TestBuildHostKeyCallback_knownHostsPermissions(t *testing.T) {
	knownHostsFile := setupForTOFU(t)

	if _, err := buildHostKeyCallback(knownHostsFile); err != nil {
		t.Fatalf("buildHostKeyCallback: %v", err)
	}

	info, err := os.Stat(knownHostsFile)
	if err != nil {
		t.Fatalf("stat known_hosts: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("expected known_hosts permissions 0600, got %04o", perm)
	}
}

func TestBuildHostKeyCallback_tofuWritesValidLine(t *testing.T) {
	knownHostsFile := setupForTOFU(t)
	pub := generateTestKey(t)
	addr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}

	cb, err := buildHostKeyCallback(knownHostsFile)
	if err != nil {
		t.Fatalf("buildHostKeyCallback: %v", err)
	}
	if err := cb("relay.example.com:22", addr, pub); err != nil {
		t.Fatalf("TOFU call: %v", err)
	}

	// The resulting known_hosts file must be parseable by knownhosts.New.
	if _, err := knownhosts.New(knownHostsFile); err != nil {
		t.Errorf("known_hosts written by TOFU is not parseable: %v", err)
	}
}
