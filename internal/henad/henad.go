// Package henad adds optional, offline AI insights to CloudRipper.
//
// Henad explains cost spikes, suggests savings, and answers questions about a
// CloudRipper report. It runs only on the Ollama model on this computer
// (http://localhost:11434). The client refuses any endpoint that is not a
// loopback address, ignores HTTP proxies, and does not follow redirects, so
// report data cannot leave the machine through it.
//
// Henad is off by default. Turning it on requires the user to type the
// confirmation phrase "I understand" after reading the warning (WarningLines).
// The opt-in is stored in a small JSON file and can be turned off at any time.
package henad

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ConfirmPhrase is what the user must type to turn Henad on.
const ConfirmPhrase = "I understand"

// DefaultEndpoint is local Ollama.
const DefaultEndpoint = "http://localhost:11434"

// EnvConfig overrides the settings file path.
const EnvConfig = "CLOUDRIPPER_HENAD_CONFIG"

// EnvEndpoint overrides the Ollama URL. It must still be a loopback address.
const EnvEndpoint = "CLOUDRIPPER_HENAD_OLLAMA"

// Henad was called Nous until Nov 2026. The old variable names and settings
// file are still honored so nobody has to turn it on again.
const (
	legacyEnvConfig    = "CLOUDRIPPER_NOUS_CONFIG"
	legacyEnvEndpoint  = "CLOUDRIPPER_NOUS_OLLAMA"
	legacySettingsFile = "nous.json"
)

// Slogan is the Henad tagline.
const Slogan = "The architecture of understanding."

var (
	// ErrNotConfirmed means the confirmation phrase did not match.
	ErrNotConfirmed = errors.New(`Henad was not turned on: type "I understand" to confirm`)
	// ErrDisabled means Henad is off.
	ErrDisabled = errors.New(`Henad is off. Turn it on with "cloudripper henad enable" (or the Henad switch in the desktop app)`)
	// ErrNotLocal means the endpoint is not on this machine.
	ErrNotLocal = errors.New("Henad only talks to Ollama on this computer (localhost, 127.0.0.1 or ::1)")
	// ErrOllamaDown means nothing answered at the Ollama endpoint.
	ErrOllamaDown = errors.New("Ollama is not running on this computer. Start the Ollama app (or run `ollama serve`), then try again")
	// ErrNoModel means Ollama answered but has no chat model.
	ErrNoModel = errors.New("Ollama has no chat model installed. Run `ollama pull llama3.2`, then try again")
)

// PhraseMatches reports whether s is the confirmation phrase, ignoring case
// and surrounding spaces.
func PhraseMatches(s string) bool {
	return strings.EqualFold(strings.TrimSpace(s), ConfirmPhrase)
}

// WarningTitle heads the warning window and the CLI prompt.
const WarningTitle = "Turn on Henad?"

// WarningLines is the text shown before Henad is turned on, in the desktop
// modal and in the CLI.
func WarningLines() []string {
	return []string{
		"You're turning on Henad, an AI assistant that explains cost spikes, suggests savings, and answers questions about your cloud inventory.",
		"Henad uses the Ollama model on this computer (" + DefaultEndpoint + "). CloudRipper only talks to Ollama on localhost; it never calls a cloud AI service.",
		"What it reads: the CloudRipper report you ask about (resource names, IDs, types, regions, status, sizes, estimated and actual costs, and savings recommendations) and recent cost-watch samples. It does not read your cloud credentials.",
		"Nothing is sent off this machine. No CloudRipper server, no third party, no telemetry.",
		`You can turn it off at any time: "cloudripper henad disable", or the Henad switch in the desktop app.`,
	}
}

// Settings is the stored opt-in.
type Settings struct {
	Enabled   bool   `json:"enabled"`
	Model     string `json:"model,omitempty"`
	EnabledAt string `json:"enabled_at,omitempty"`
}

// DefaultPath is <user config dir>/cloudripper/henad.json, or $CLOUDRIPPER_HENAD_CONFIG
// ($CLOUDRIPPER_NOUS_CONFIG still works). If only the pre-rename nous.json
// exists, that file keeps being used.
func DefaultPath() (string, error) {
	for _, name := range []string{EnvConfig, legacyEnvConfig} {
		if p := strings.TrimSpace(os.Getenv(name)); p != "" {
			return p, nil
		}
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find config folder: %w", err)
	}
	return settingsPathIn(filepath.Join(dir, "cloudripper")), nil
}

func settingsPathIn(dir string) string {
	current := filepath.Join(dir, "henad.json")
	if _, err := os.Stat(current); err == nil {
		return current
	}
	legacy := filepath.Join(dir, legacySettingsFile)
	if _, err := os.Stat(legacy); err == nil {
		return legacy
	}
	return current
}

// Load reads the settings. A missing file means Henad is off.
func Load(path string) (Settings, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Settings{}, nil
	}
	if err != nil {
		return Settings{}, err
	}
	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return Settings{}, fmt.Errorf("read %s: %w", path, err)
	}
	return s, nil
}

// Save writes the settings with owner-only permissions.
func Save(path string, s Settings) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Enable turns Henad on only when phrase matches ConfirmPhrase.
func Enable(path, phrase, model string, now time.Time) (Settings, error) {
	if !PhraseMatches(phrase) {
		return Settings{}, ErrNotConfirmed
	}
	s := Settings{Enabled: true, Model: strings.TrimSpace(model), EnabledAt: now.UTC().Format(time.RFC3339)}
	if err := Save(path, s); err != nil {
		return Settings{}, err
	}
	return s, nil
}

// Disable turns Henad off and keeps the chosen model.
func Disable(path string) error {
	s, err := Load(path)
	if err != nil {
		s = Settings{}
	}
	s.Enabled = false
	s.EnabledAt = ""
	return Save(path, s)
}

// Endpoint is $CLOUDRIPPER_HENAD_OLLAMA ($CLOUDRIPPER_NOUS_OLLAMA still works) or DefaultEndpoint.
func Endpoint() string {
	for _, name := range []string{EnvEndpoint, legacyEnvEndpoint} {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v
		}
	}
	return DefaultEndpoint
}

// ValidateEndpoint accepts only http(s) URLs on a loopback host.
func ValidateEndpoint(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ErrNotLocal
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return ErrNotLocal
	}
	return nil
}

// Message is one chat turn.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Client talks to local Ollama.
type Client struct {
	endpoint string
	http     *http.Client
}

// NewClient returns a client for a loopback Ollama endpoint.
func NewClient(endpoint string) (*Client, error) {
	if err := ValidateEndpoint(endpoint); err != nil {
		return nil, err
	}
	transport := &http.Transport{
		Proxy:                 nil, // never route through a proxy
		DialContext:           (&net.Dialer{Timeout: 3 * time.Second}).DialContext,
		ResponseHeaderTimeout: 5 * time.Minute,
	}
	return &Client{
		endpoint: strings.TrimRight(strings.TrimSpace(endpoint), "/"),
		http: &http.Client{
			Transport: transport,
			Timeout:   6 * time.Minute,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return errors.New("Henad does not follow redirects")
			},
		},
	}, nil
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrOllamaDown
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusNotFound && strings.Contains(string(data), "model") {
		return ErrNoModel
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("Ollama returned HTTP %d", resp.StatusCode)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("Ollama sent an unexpected reply: %w", err)
	}
	return nil
}

// Models lists installed model names (GET /api/tags).
func (c *Client) Models(ctx context.Context) ([]string, error) {
	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/tags", nil, &tags); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(tags.Models))
	for _, m := range tags.Models {
		if m.Name != "" {
			names = append(names, m.Name)
		}
	}
	return names, nil
}

// PickModel returns preferred when installed, otherwise the first chat
// model (embedding models are skipped). Empty when there is none.
func PickModel(models []string, preferred string) string {
	preferred = strings.TrimSpace(preferred)
	if preferred != "" {
		for _, m := range models {
			if m == preferred || m == preferred+":latest" {
				return m
			}
		}
	}
	for _, m := range models {
		lower := strings.ToLower(m)
		if strings.Contains(lower, "embed") || strings.Contains(lower, "bert") {
			continue
		}
		return m
	}
	return ""
}

// Chat sends messages to Ollama (POST /api/chat, not streamed).
func (c *Client) Chat(ctx context.Context, model string, messages []Message) (string, error) {
	req := map[string]any{
		"model":    model,
		"messages": messages,
		"stream":   false,
		"options":  map[string]any{"temperature": 0.2},
	}
	var resp struct {
		Message Message `json:"message"`
		Error   string  `json:"error"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/chat", req, &resp); err != nil {
		return "", err
	}
	if resp.Error != "" {
		return "", fmt.Errorf("Ollama: %s", resp.Error)
	}
	text := strings.TrimSpace(resp.Message.Content)
	if text == "" {
		return "", errors.New("Ollama returned an empty answer")
	}
	return text, nil
}
