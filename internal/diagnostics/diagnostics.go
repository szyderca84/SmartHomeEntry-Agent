// Package diagnostics zbiera informacje o tym, dlaczego agent nie moze
// dosiegnac uslugi lokalnej, i przesyla je do control plane.
//
// Zasada: raportujemy stan, nigdy nie przyjmujemy polecen. Serwer nie moze
// na tej podstawie zmienic tego, co agent wystawia - o tym decyduje wylacznie
// wlasciciel maszyny przez agent.env. Patrz docs/agent-diagnostics.md w
// repozytorium control plane.
package diagnostics

import (
	"context"
	"errors"
	"net"
	"os"
	"sort"
	"sync"
	"time"
)

// Zamknieta lista portow typowych dla uslug, ktore ludzie tuneluja. Celowo
// krotka i statyczna: agent ma pomoc wskazac wlasciwy port, a nie skanowac
// siec klienta.
var commonPorts = []int{80, 443, 1880, 3000, 6052, 8080, 8081, 8096, 8123, 8443, 8581, 9000}

const probeTimeout = 300 * time.Millisecond

// Report trafia do control plane w polu "diagnostics" heartbeatu.
type Report struct {
	LocalReachable  bool   `json:"local_reachable"`
	LocalErrorClass string `json:"local_error_class"`
	LocalAddrUsed   string `json:"local_addr_used"`
	ListeningPorts  []int  `json:"listening_ports"`
}

// Enabled mowi, czy uzytkownik nie wylaczyl diagnostyki w agent.env.
func Enabled() bool {
	return os.Getenv("SMARTHOMEENTRY_DIAGNOSTICS") != "off"
}

// Recorder przechowuje wynik ostatniej proby polaczenia z usluga lokalna.
// proxyConn wola Record z kazdego zadania, petla heartbeatu czyta Snapshot.
type Recorder struct {
	mu        sync.Mutex
	reachable bool
	class     string
	seen      bool
}

func NewRecorder() *Recorder { return &Recorder{} }

func (r *Recorder) Record(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = true
	r.reachable = err == nil
	r.class = ClassifyDialError(err)
}

// Snapshot zwraca ostatni wynik oraz informacje, czy w ogole byla jakas proba.
func (r *Recorder) Snapshot() (reachable bool, class string, seen bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reachable, r.class, r.seen
}

// ClassifyDialError sprowadza blad polaczenia do jednej z kilku klas.
// Nie przekazujemy surowego bledu: moze zawierac nazwy hostow z sieci klienta.
func ClassifyDialError(err error) string {
	if err == nil {
		return "ok"
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "dns"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Err != nil {
		// "connection refused" to najczestszy przypadek: usluga nie stoi na
		// tym porcie. Rozrozniamy go, bo w panelu prowadzi do innej porady
		// niz timeout (ten sugeruje firewall albo zly host).
		if opErr.Err.Error() == "connect: connection refused" {
			return "refused"
		}
	}
	return "other"
}

// ProbePorts sprawdza, ktore z commonPorts nasluchuja na podanych hostach.
//
// hosts jest celowo ograniczone przez wolajacego do petli zwrotnej i hosta z
// local_addr. Agent nie moze stac sie narzedziem do rekonesansu LAN-u.
func ProbePorts(ctx context.Context, hosts []string) []int {
	found := make(map[int]struct{})
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, host := range hosts {
		if host == "" {
			continue
		}
		for _, port := range commonPorts {
			wg.Add(1)
			go func(h string, p int) {
				defer wg.Done()
				d := net.Dialer{Timeout: probeTimeout}
				conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(h, itoa(p)))
				if err != nil {
					return
				}
				// Zamykamy natychmiast - interesuje nas wylacznie fakt, ze
				// cos tam nasluchuje. Zadnych bannerow ani odczytow.
				_ = conn.Close()
				mu.Lock()
				found[p] = struct{}{}
				mu.Unlock()
			}(host, port)
		}
	}
	wg.Wait()

	ports := make([]int, 0, len(found))
	for p := range found {
		ports = append(ports, p)
	}
	sort.Ints(ports)
	return ports
}

// ProbeHosts wyznacza dozwolony zbior hostow do sprawdzenia: zawsze petla
// zwrotna, dodatkowo host z local_addr, jesli jest inny.
func ProbeHosts(localAddr string) []string {
	hosts := []string{"127.0.0.1"}
	if h, _, err := net.SplitHostPort(localAddr); err == nil && h != "" {
		if h != "127.0.0.1" && h != "localhost" {
			hosts = append(hosts, h)
		}
	}
	return hosts
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [6]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
