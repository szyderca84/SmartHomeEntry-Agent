package agent

import (
	"context"
	"net"
	"os"
	"testing"
	"time"
)

func TestSleepCtx_timesOut(t *testing.T) {
	ctx := context.Background()
	start := time.Now()
	result := sleepCtx(ctx, 50*time.Millisecond)
	elapsed := time.Since(start)

	if !result {
		t.Error("expected true when timer fires normally")
	}
	if elapsed < 40*time.Millisecond {
		t.Errorf("sleepCtx returned too early: elapsed=%v", elapsed)
	}
}

func TestSleepCtx_contextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	result := sleepCtx(ctx, 10*time.Second)
	elapsed := time.Since(start)

	if result {
		t.Error("expected false when context is cancelled")
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("sleepCtx took too long after cancel: elapsed=%v", elapsed)
	}
}

func TestSleepCtx_alreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := sleepCtx(ctx, 10*time.Second)
	if result {
		t.Error("expected false for already-cancelled context")
	}
}

func TestCheckDomoticz_unreachable(t *testing.T) {
	checkDomoticz("127.0.0.1:1")
}

func TestCheckDomoticz_reachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("cannot start test listener: %v", err)
	}
	defer ln.Close()

	checkDomoticz(ln.Addr().String())
}

func TestWriteKey_createsFileWith0600(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("requires root")
	}

	const testKey = "-----BEGIN OPENSSH PRIVATE KEY-----\ntest-key-data\n-----END OPENSSH PRIVATE KEY-----\n"

	if err := writeKey(testKey); err != nil {
		t.Fatalf("writeKey: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(keyFilePath) })

	info, err := os.Stat(keyFilePath)
	if err != nil {
		t.Fatalf("stat %s: %v", keyFilePath, err)
	}

	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("expected permissions 0600, got %04o", perm)
	}

	content, err := os.ReadFile(keyFilePath)
	if err != nil {
		t.Fatalf("read %s: %v", keyFilePath, err)
	}
	if string(content) != testKey {
		t.Errorf("key content mismatch:\ngot:  %q\nwant: %q", string(content), testKey)
	}
}

func TestWriteKey_overwritesPreviousKey(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("requires root")
	}

	first := "first-key\n"
	second := "second-key\n"

	if err := writeKey(first); err != nil {
		t.Fatalf("first writeKey: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(keyFilePath) })

	if err := writeKey(second); err != nil {
		t.Fatalf("second writeKey: %v", err)
	}

	content, err := os.ReadFile(keyFilePath)
	if err != nil {
		t.Fatalf("read %s: %v", keyFilePath, err)
	}
	if string(content) != second {
		t.Errorf("expected second key only, got: %q", string(content))
	}
}
