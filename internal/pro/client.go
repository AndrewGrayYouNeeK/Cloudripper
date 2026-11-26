// Package pro uploads CLI JSON reports to a CloudRipper Pro server.
// Cloud credentials never leave the machine; only the report JSON is sent.
package pro

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Environment variables read by the CLI.
const (
	EnvURL   = "CLOUDRIPPER_PRO_URL"
	EnvToken = "CLOUDRIPPER_PRO_TOKEN"
)

// TokenPrefix is the prefix of every Pro API token.
const TokenPrefix = "crp_"

// MaxReportBytes matches the server's upload limit.
const MaxReportBytes = 5 * 1024 * 1024

// Upload is the request body for POST /api/v1/reports.
type Upload struct {
	Account     string          `json:"account"`
	Kind        string          `json:"kind,omitempty"`
	GeneratedAt string          `json:"generated_at,omitempty"`
	Report      json.RawMessage `json:"report"`
}

// Stored is the server's summary of an accepted report.
type Stored struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Account struct {
		ID    string `json:"id"`
		Label string `json:"label"`
	} `json:"account"`
	ResourceCount       *int     `json:"resource_count"`
	EstimatedTotal      *float64 `json:"estimated_total"`
	ActualTotal         *float64 `json:"actual_total"`
	PotentialSavings    *float64 `json:"potential_savings"`
	RecommendationCount *int     `json:"recommendation_count"`
}

// Error is a non-2xx response from the server.
type Error struct {
	StatusCode int
	Message    string
}

func (e *Error) Error() string {
	return fmt.Sprintf("pro upload failed (HTTP %d): %s", e.StatusCode, e.Message)
}

// Permanent reports whether retrying the same request cannot succeed
// (bad token, inactive subscription, or invalid report).
func (e *Error) Permanent() bool {
	return e.StatusCode >= 400 && e.StatusCode < 500 && e.StatusCode != http.StatusTooManyRequests
}

// IsPermanent reports whether err is a permanent server rejection.
func IsPermanent(err error) bool {
	var perr *Error
	return errors.As(err, &perr) && perr.Permanent()
}

// Client talks to one Pro server.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// NewClient validates the endpoint and token.
func NewClient(baseURL, token string) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("set %s or pass --endpoint to your CloudRipper Pro URL", EnvURL)
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && !isLoopback(u.Hostname())) {
		return nil, fmt.Errorf("pro endpoint must be an https URL (http is allowed only for localhost): %q", baseURL)
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("set %s to an API token from the Pro dashboard", EnvToken)
	}
	if !strings.HasPrefix(token, TokenPrefix) {
		return nil, fmt.Errorf("%s does not look like a Pro API token (expected %s...)", EnvToken, TokenPrefix)
	}
	return &Client{BaseURL: baseURL, Token: token, HTTP: &http.Client{Timeout: 60 * time.Second}}, nil
}

func isLoopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// Upload sends one report and returns the server's summary.
func (c *Client) Upload(ctx context.Context, up Upload) (Stored, error) {
	body, err := json.Marshal(up)
	if err != nil {
		return Stored{}, err
	}
	if len(body) > MaxReportBytes {
		return Stored{}, fmt.Errorf("report is %d bytes; the Pro upload limit is %d bytes", len(body), MaxReportBytes)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/v1/reports", bytes.NewReader(body))
	if err != nil {
		return Stored{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Stored{}, fmt.Errorf("pro upload: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var parsed struct {
		OK     bool   `json:"ok"`
		Error  string `json:"error"`
		Report Stored `json:"report"`
	}
	_ = json.Unmarshal(raw, &parsed)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg := parsed.Error
		if msg == "" {
			msg = strings.TrimSpace(string(raw))
			if len(msg) > 200 {
				msg = msg[:200]
			}
		}
		return Stored{}, &Error{StatusCode: resp.StatusCode, Message: msg}
	}
	if parsed.Report.ID == "" {
		return Stored{}, fmt.Errorf("pro upload: unexpected response from server")
	}
	return parsed.Report, nil
}

// InferKind guesses the report kind from CLI JSON output: rip/cost output has
// "resources"; optimize output has "Recommendations" or "recommendations".
// rip and cost share one shape, so callers pass --kind cost to label cost output.
func InferKind(report []byte) (string, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(report, &probe); err != nil {
		return "", fmt.Errorf("report is not a JSON object: %w", err)
	}
	if _, ok := probe["resources"]; ok {
		return "rip", nil
	}
	if _, ok := probe["Recommendations"]; ok {
		return "optimize", nil
	}
	if _, ok := probe["recommendations"]; ok {
		return "optimize", nil
	}
	return "", errors.New("report does not look like rip, cost, or optimize --output json")
}
