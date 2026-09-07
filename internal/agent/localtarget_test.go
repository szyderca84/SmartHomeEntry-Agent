package agent

import "testing"

// Adres lokalny przychodzi z control plane i trafia prosto do net.Dial, wiec
// walidacja jest jedyna barriera miedzy panelem a polaczeniem sieciowym agenta.
func TestValidLocalAddr(t *testing.T) {
	valid := []string{
		"localhost:80",
		"localhost:8123",
		"192.168.1.47:443",
		"nextcloud.local:8080",
		"host-with-dash.lan:1",
		"[::1]:80",
		"127.0.0.1:65535",
	}
	for _, addr := range valid {
		if !validLocalAddr(addr) {
			t.Errorf("validLocalAddr(%q) = false, oczekiwano true", addr)
		}
	}

	invalid := []string{
		"",                    // puste
		"localhost",           // brak portu - najczestsza pomylka
		"localhost:",          // pusty port
		":80",                 // brak hosta
		"localhost:0",         // port 0
		"localhost:70000",     // poza zakresem
		"localhost:-1",        // ujemny
		"localhost:abc",       // nienumeryczny
		"localhost:80:80",     // dwa porty
		"http://localhost:80", // schemat, nie host:port
	}
	for _, addr := range invalid {
		if validLocalAddr(addr) {
			t.Errorf("validLocalAddr(%q) = true, oczekiwano false", addr)
		}
	}
}

// Odwzorowanie logiki pierwszenstwa z runCycle: panel wygrywa, ale tylko gdy
// przysyla wartosc sensowna. Inaczej zostaje to, co w agent.env.
func resolveLocalAddr(fromEnv, fromControlPlane string) string {
	if fromControlPlane != "" && validLocalAddr(fromControlPlane) {
		return fromControlPlane
	}
	return fromEnv
}

func TestResolveLocalAddrPrecedence(t *testing.T) {
	cases := []struct {
		name, env, cp, want string
	}{
		{"panel nadpisuje env", "localhost:8080", "192.168.1.47:443", "192.168.1.47:443"},
		{"starszy control plane nie przysyla pola", "localhost:8123", "", "localhost:8123"},
		{"smieciowa wartosc nie psuje dzialajacego agenta", "localhost:8123", "nonsens", "localhost:8123"},
		{"brak portu odrzucony", "localhost:8123", "192.168.1.47", "localhost:8123"},
		{"ta sama wartosc", "localhost:80", "localhost:80", "localhost:80"},
	}
	for _, c := range cases {
		if got := resolveLocalAddr(c.env, c.cp); got != c.want {
			t.Errorf("%s: resolveLocalAddr(%q, %q) = %q, oczekiwano %q", c.name, c.env, c.cp, got, c.want)
		}
	}
}
