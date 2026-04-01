package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ErrUnauthorized is returned when the control plane rejects our token (HTTP 401/403).
var ErrUnauthorized = errors.New("unauthorized: install token rejected by control plane")

type AgentConfig struct {
	Host         string `json:"host"`
	Port         int    `json:"port"`
	TunnelPort   int    `json:"tunnel_port"`
	SSHUser      string `json:"ssh_user"`
	PrivateKey   string `json:"private_key"`
	Active       bool   `json:"active"`
	HeartbeatURL string `json:"heartbeat_url"`
}

type HeartbeatResponse struct {
	Active bool `json:"active"`
}

type HeartbeatMetrics struct {
	CPUPercent float64 `json:"cpu_percent"`
	RAMPercent float64 `json:"ram_percent"`
	RAMUsedMB  int     `json:"ram_used_mb"`
	RAMTotalMB int     `json:"ram_total_mb"`
}

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

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
		return ErrUnauthorized
	default:
		return fmt.Errorf("validate token: unexpected HTTP %d", resp.StatusCode)
	}
}

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

// SendHeartbeat POSTs to heartbeatURL. On transient errors, returns active=true
// to avoid accidentally closing a healthy tunnel.
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
	hbr.Active = true
	_ = json.NewDecoder(resp.Body).Decode(&hbr)
	return &hbr, nil
}
