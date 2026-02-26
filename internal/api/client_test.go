package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newTestClient builds a Client that talks to baseURL without the HTTPS check.
func newTestClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		token:   "test-token",
		http:    &http.Client{Timeout: 5 * time.Second},
	}
}

// ---------- New ----------

func TestNew_requiresHTTPS(t *testing.T) {
	_, err := New("http://example.com", "tok")
	if err == nil {
		t.Fatal("expected error for plain HTTP URL, got nil")
	}
}

func TestNew_acceptsHTTPS(t *testing.T) {
	c, err := New("https://example.com", "tok")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.baseURL != "https://example.com" {
		t.Errorf("baseURL=%q, want %q", c.baseURL, "https://example.com")
	}
}

func TestNew_trailingSlashStripped(t *testing.T) {
	c, err := New("https://example.com/", "tok")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.baseURL != "https://example.com" {
		t.Errorf("baseURL=%q, expected trailing slash stripped", c.baseURL)
	}
}

// ---------- ValidateToken ----------

func TestValidateToken_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/agent/validate" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("unexpected Authorization: %s", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	if err := c.ValidateToken(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateToken_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	if err := c.ValidateToken(context.Background()); err == nil {
		t.Fatal("expected error for 401, got nil")
	}
}

func TestValidateToken_Forbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	if err := c.ValidateToken(context.Background()); err == nil {
		t.Fatal("expected error for 403, got nil")
	}
}

func TestValidateToken_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	if err := c.ValidateToken(context.Background()); err == nil {
		t.Fatal("expected error for 500, got nil")
	}
}

func TestValidateToken_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Slow response — should be cancelled by context.
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	c := newTestClient(srv.URL)
	if err := c.ValidateToken(ctx); err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}

// ---------- FetchConfig ----------

func validConfig() AgentConfig {
	return AgentConfig{
		Host:         "relay.example.com",
		Port:         22,
		TunnelPort:   9000,
		SSHUser:      "agent",
		PrivateKey:   "-----BEGIN OPENSSH PRIVATE KEY-----",
		Active:       true,
		HeartbeatURL: "https://api.example.com/heartbeat",
	}
}

func TestFetchConfig_OK(t *testing.T) {
	cfg := validConfig()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agent/config" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("unexpected Authorization: %s", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(cfg)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	got, err := c.FetchConfig(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Host != cfg.Host {
		t.Errorf("Host: got %q, want %q", got.Host, cfg.Host)
	}
	if got.Port != cfg.Port {
		t.Errorf("Port: got %d, want %d", got.Port, cfg.Port)
	}
	if got.TunnelPort != cfg.TunnelPort {
		t.Errorf("TunnelPort: got %d, want %d", got.TunnelPort, cfg.TunnelPort)
	}
	if got.Active != cfg.Active {
		t.Errorf("Active: got %v, want %v", got.Active, cfg.Active)
	}
	if got.HeartbeatURL != cfg.HeartbeatURL {
		t.Errorf("HeartbeatURL: got %q, want %q", got.HeartbeatURL, cfg.HeartbeatURL)
	}
}

func TestFetchConfig_Inactive(t *testing.T) {
	cfg := validConfig()
	cfg.Active = false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(cfg)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	got, err := c.FetchConfig(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Active {
		t.Error("expected active=false")
	}
}

func TestFetchConfig_MissingHost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(AgentConfig{Port: 22, TunnelPort: 9000})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.FetchConfig(context.Background())
	if err == nil {
		t.Fatal("expected error for missing host")
	}
}

func TestFetchConfig_MissingPort(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(AgentConfig{Host: "relay.example.com", TunnelPort: 9000})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.FetchConfig(context.Background())
	if err == nil {
		t.Fatal("expected error for missing port")
	}
}

func TestFetchConfig_MissingTunnelPort(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(AgentConfig{Host: "relay.example.com", Port: 22})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.FetchConfig(context.Background())
	if err == nil {
		t.Fatal("expected error for missing tunnel_port")
	}
}

func TestFetchConfig_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.FetchConfig(context.Background())
	if err == nil {
		t.Fatal("expected error for 401")
	}
}

func TestFetchConfig_Forbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.FetchConfig(context.Background())
	if err == nil {
		t.Fatal("expected error for 403")
	}
}

func TestFetchConfig_UnexpectedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.FetchConfig(context.Background())
	if err == nil {
		t.Fatal("expected error for 503")
	}
}

func TestFetchConfig_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("this is not json"))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.FetchConfig(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid JSON body")
	}
}

// ---------- SendHeartbeat ----------

func TestSendHeartbeat_ActiveTrue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		_ = json.NewEncoder(w).Encode(HeartbeatResponse{Active: true})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	resp, err := c.SendHeartbeat(context.Background(), srv.URL+"/heartbeat", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Active {
		t.Error("expected active=true")
	}
}

func TestSendHeartbeat_ActiveFalse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(HeartbeatResponse{Active: false})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	resp, err := c.SendHeartbeat(context.Background(), srv.URL+"/heartbeat", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Active {
		t.Error("expected active=false")
	}
}

// Empty 200 body must default to active=true to avoid accidentally closing a healthy tunnel.
func TestSendHeartbeat_EmptyBodyDefaultsToActive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK) // no body
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	resp, err := c.SendHeartbeat(context.Background(), srv.URL+"/heartbeat", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Active {
		t.Error("empty 200 response must default to active=true (fail-safe)")
	}
}

func TestSendHeartbeat_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.SendHeartbeat(context.Background(), srv.URL+"/heartbeat", nil)
	if err == nil {
		t.Fatal("expected error for 503")
	}
}

func TestSendHeartbeat_AuthHeaderSent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("unexpected Authorization: %q", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(HeartbeatResponse{Active: true})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.SendHeartbeat(context.Background(), srv.URL+"/hb", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
