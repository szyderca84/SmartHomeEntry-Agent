package diagnostics

import (
	"context"
	"errors"
	"net"
	"os"
	"testing"
)

func TestClassifyDialError(t *testing.T) {
	if got := ClassifyDialError(nil); got != "ok" {
		t.Errorf("nil -> %q, oczekiwano ok", got)
	}
	if got := ClassifyDialError(&net.DNSError{Err: "no such host", IsNotFound: true}); got != "dns" {
		t.Errorf("DNSError -> %q, oczekiwano dns", got)
	}
	timeoutErr := &net.OpError{Op: "dial", Err: &timeoutError{}}
	if got := ClassifyDialError(timeoutErr); got != "timeout" {
		t.Errorf("timeout -> %q, oczekiwano timeout", got)
	}
	refused := &net.OpError{Op: "dial", Err: errors.New("connect: connection refused")}
	if got := ClassifyDialError(refused); got != "refused" {
		t.Errorf("refused -> %q, oczekiwano refused", got)
	}
	if got := ClassifyDialError(errors.New("cos innego")); got != "other" {
		t.Errorf("nieznany -> %q, oczekiwano other", got)
	}
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

// Najwazniejszy test w tym pakiecie: agent nie moze skanowac sieci klienta.
// Dozwolona jest petla zwrotna i host z local_addr - nic wiecej.
func TestProbeHostsNieWychodziPozaDozwolone(t *testing.T) {
	cases := []struct {
		localAddr string
		want      []string
	}{
		{"localhost:8080", []string{"127.0.0.1"}},
		{"127.0.0.1:443", []string{"127.0.0.1"}},
		{"192.168.1.47:443", []string{"127.0.0.1", "192.168.1.47"}},
		{"nextcloud.local:80", []string{"127.0.0.1", "nextcloud.local"}},
		{"", []string{"127.0.0.1"}},
		{"bezportu", []string{"127.0.0.1"}},
	}
	for _, c := range cases {
		got := ProbeHosts(c.localAddr)
		if len(got) != len(c.want) {
			t.Errorf("ProbeHosts(%q) = %v, oczekiwano %v", c.localAddr, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("ProbeHosts(%q) = %v, oczekiwano %v", c.localAddr, got, c.want)
				break
			}
		}
		if len(got) > 2 {
			t.Errorf("ProbeHosts(%q) zwrocil %d hostow - limit to 2", c.localAddr, len(got))
		}
	}
}

func TestProbePortsZnajdujeNasluchujacyPort(t *testing.T) {
	// 9000 jest na liscie commonPorts
	ln, err := net.Listen("tcp", "127.0.0.1:9000")
	if err != nil {
		t.Skip("port 9000 zajety w srodowisku testowym")
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()

	ports := ProbePorts(context.Background(), []string{"127.0.0.1"})
	found := false
	for _, p := range ports {
		if p == 9000 {
			found = true
		}
	}
	if !found {
		t.Errorf("nie wykryto nasluchu na 9000, dostalem %v", ports)
	}
}

func TestRecorder(t *testing.T) {
	r := NewRecorder()
	if _, _, seen := r.Snapshot(); seen {
		t.Error("swiezy recorder nie powinien raportowac zadnej proby")
	}
	r.Record(errors.New("boom"))
	reachable, class, seen := r.Snapshot()
	if !seen || reachable || class != "other" {
		t.Errorf("po bledzie: reachable=%v class=%q seen=%v", reachable, class, seen)
	}
	r.Record(nil)
	reachable, class, _ = r.Snapshot()
	if !reachable || class != "ok" {
		t.Errorf("po sukcesie: reachable=%v class=%q", reachable, class)
	}
}

func TestEnabledDomyslnieWlaczone(t *testing.T) {
	os.Unsetenv("SMARTHOMEENTRY_DIAGNOSTICS")
	if !Enabled() {
		t.Error("domyslnie diagnostyka ma byc wlaczona")
	}
	os.Setenv("SMARTHOMEENTRY_DIAGNOSTICS", "off")
	defer os.Unsetenv("SMARTHOMEENTRY_DIAGNOSTICS")
	if Enabled() {
		t.Error("wartosc off ma wylaczac diagnostyke")
	}
}
