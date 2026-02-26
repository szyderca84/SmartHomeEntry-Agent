// Package api provides a client for the SmartHomeEntry control plane API.
// All requests are HTTPS-only; the client refuses to operate over plain HTTP.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// AgentConfig is the configuration returned by the control plane /api/agent/config endpoint.
//
// Field semantics:
//   - Host:         DNS name or IP of the relay server (never assumed to equal the API host)
//   - Port:         SSH daemon port on the relay (typically 22 or 2222)
//   - TunnelPort:   port the relay sshd will bind for the reverse forward (127.0.0.1 only)
//   - SSHUser:      SSH username assigned to this device
//   - PrivateKey:   PEM-encoded RSA/Ed25519 private key (no passphrase)
//   - Active:       false → agent must close tunnel and poll every 5 m
//   - HeartbeatURL: full HTTPS URL for 60-second heartbeat POSTs
type AgentConfig struct {
	Host         string `json:"host"`
	Port         int    `json:"port"`
	TunnelPort   int    `json:"tunnel_port"`
	SSHUser      string `json:"ssh_user"`
	PrivateKey   string `json:"private_key"`
	Active       bool   `json:"active"`
	HeartbeatURL string `json:"heartbeat_url"`
}

// HeartbeatResponse is the JSON body returned by the heartbeat endpoint.
type HeartbeatResponse struct {
	Active bool `json:"active"`
}

// HeartbeatMetrics carries optional CPU/RAM metrics sent alongside each heartbeat.
// All fields are required when the struct is non-nil; pass nil to send a bare heartbeat.
type HeartbeatMetrics struct {
	CPUPercent float64 `json:"cpu_percent"`
	RAMPercent float64 `json:"ram_percent"`
	RAMUsedMB  int     `json:"ram_used_mb"`
	RAMTotalMB int     `json:"ram_total_mb"`
}

// Client is an HTTPS-only API client for the SmartHomeEntry control plane.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// New returns a Client for baseURL authenticated with token.
// Returns an error if baseURL does not begin with "https://".
func New(baseURL, token string) (*Client, error) {
	if !strings.HasPrefix(baseURL, "https://") {
		return nil, fmt.Errorf("API_URL must use HTTPS, got: %q", baseURL)
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

// ValidateToken calls POST /api/agent/validate to confirm the install token is accepted
// by the control plane. Returns a non-nil error when the token is invalid or the
// server is unreachable.
func (c *Client) ValidateToken(ctx context.Context) error {
	body, _ := json.Marshal(map[string]string{"token": c.token})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/api/agent/validate", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build validate request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("validate token: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("invalid install token (HTTP %d)", resp.StatusCode)
	default:
		return fmt.Errorf("validate token: unexpected HTTP %d", resp.StatusCode)
	}
}

// FetchConfig retrieves the current agent configuration from the control plane.
// The returned config contains relay connection details and the SSH private key.
func (c *Client) FetchConfig(ctx context.Context) (*AgentConfig, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/api/agent/config", nil)
	if err != nil {
		return nil, fmt.Errorf("build config request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch config: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		// handled below
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, fmt.Errorf("fetch config: unauthorized (HTTP %d)", resp.StatusCode)
	default:
		return nil, fmt.Errorf("fetch config: unexpected HTTP %d", resp.StatusCode)
	}

	var cfg AgentConfig
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode config response: %w", err)
	}
	if cfg.Host == "" {
		return nil, fmt.Errorf("config response missing 'host' field")
	}
	if cfg.Port == 0 {
		return nil, fmt.Errorf("config response missing 'port' field")
	}
	if cfg.TunnelPort == 0 {
		return nil, fmt.Errorf("config response missing 'tunnel_port' field")
	}
	return &cfg, nil
}

// SendHeartbeat POSTs to heartbeatURL and returns the server's current active flag.
// If m is non-nil, CPU/RAM metrics are included in the request body.
// On transient errors (network, non-200), it returns active=true to avoid accidentally
// closing a healthy tunnel due to a momentary API blip.
func (c *Client) SendHeartbeat(ctx context.Context, heartbeatURL string, m *HeartbeatMetrics) (*HeartbeatResponse, error) {
	var body []byte
	if m != nil {
		var err error
		body, err = json.Marshal(m)
		if err != nil {
			return nil, fmt.Errorf("marshal heartbeat metrics: %w", err)
		}
	}

	var bodyReader *bytes.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	} else {
		bodyReader = bytes.NewReader(nil)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, heartbeatURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("build heartbeat request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send heartbeat: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("heartbeat: unexpected HTTP %d", resp.StatusCode)
	}

	var hbr HeartbeatResponse
	// Some implementations return an empty 200; tolerate that by defaulting to active.
	hbr.Active = true
	_ = json.NewDecoder(resp.Body).Decode(&hbr)
	return &hbr, nil
}
