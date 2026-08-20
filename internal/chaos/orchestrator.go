package chaos

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Provider identifies a chaos engineering backend.
type Provider string

const (
	ProviderChaosMesh Provider = "chaos-mesh"
	ProviderGremlin   Provider = "gremlin"
)

// Config holds chaos orchestrator settings.
type Config struct {
	Provider Provider
	Endpoint string
	APIKey   string
}

// Experiment describes a chaos injection request.
type Experiment struct {
	Name       string         `json:"name"`
	Kind       string         `json:"kind"`
	Target     string         `json:"target"`
	Duration   string         `json:"duration"`
	Parameters map[string]any `json:"parameters,omitempty"`
}

// Orchestrator injects chaos experiments via HTTP APIs.
type Orchestrator struct {
	config Config
	client *http.Client
}

// NewOrchestrator creates a chaos orchestrator.
func NewOrchestrator(cfg Config) *Orchestrator {
	return &Orchestrator{
		config: cfg,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// Inject runs a chaos experiment against the configured provider.
func (o *Orchestrator) Inject(ctx context.Context, exp Experiment) (map[string]any, error) {
	if o.config.Endpoint == "" {
		return nil, fmt.Errorf("chaos endpoint not configured (set CHAOS_ENDPOINT)")
	}

	switch o.config.Provider {
	case ProviderChaosMesh:
		return o.injectChaosMesh(ctx, exp)
	case ProviderGremlin:
		return o.injectGremlin(ctx, exp)
	default:
		return nil, fmt.Errorf("unknown chaos provider: %s", o.config.Provider)
	}
}

func (o *Orchestrator) injectChaosMesh(ctx context.Context, exp Experiment) (map[string]any, error) {
	payload := map[string]any{
		"apiVersion": "chaos-mesh.org/v1alpha1",
		"kind":       exp.Kind,
		"metadata": map[string]string{
			"name": exp.Name,
		},
		"spec": map[string]any{
			"selector": map[string]any{
				"labelSelectors": map[string]string{
					"app": exp.Target,
				},
			},
			"duration":   exp.Duration,
			"parameters": exp.Parameters,
		},
	}
	return o.post(ctx, buildEndpointURL(o.config.Endpoint, "/api/v1/experiments", "http"), payload, nil)
}

func (o *Orchestrator) injectGremlin(ctx context.Context, exp Experiment) (map[string]any, error) {
	payload := map[string]any{
		"targets": []map[string]string{
			{"type": exp.Target},
		},
		"command": map[string]any{
			"type":       exp.Kind,
			"parameters": exp.Parameters,
		},
	}
	headers := map[string]string{
		"Authorization": "Bearer " + o.config.APIKey,
	}
	return o.post(ctx, buildEndpointURL(o.config.Endpoint, "/v1/attacks", "https"), payload, headers)
}

func buildEndpointURL(endpoint, path, defaultScheme string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return path
	}

	// Accept host-only endpoints (chaos.example.com) and full URLs (https://…).
	if !strings.Contains(endpoint, "://") {
		endpoint = defaultScheme + "://" + strings.TrimSuffix(endpoint, "/")
	}

	parsed, err := url.Parse(endpoint)
	if err != nil {
		return strings.TrimSuffix(endpoint, "/") + path
	}

	parsed.Path = strings.TrimSuffix(parsed.Path, "/") + path
	return parsed.String()
}

// ResolveAction returns the chaos action to use, preferring an explicit override.
func ResolveAction(explicitAction, kind string) string {
	if explicitAction != "" {
		return explicitAction
	}
	return DefaultActionForKind(kind)
}

// NewExperiment builds an experiment with a valid Chaos Mesh action (never the kind name).
func NewExperiment(name, kind, target, duration, explicitAction string) Experiment {
	return Experiment{
		Name:     name,
		Kind:     kind,
		Target:   target,
		Duration: duration,
		Parameters: map[string]any{
			"action": ResolveAction(explicitAction, kind),
		},
	}
}

// DefaultActionForKind maps Chaos Mesh kinds to their default actions.
func DefaultActionForKind(kind string) string {
	switch kind {
	case "PodChaos":
		return "pod-kill"
	case "NetworkChaos":
		return "delay"
	case "StressChaos":
		return "cpu"
	default:
		return ""
	}
}

func (o *Orchestrator) post(ctx context.Context, rawURL string, payload map[string]any, headers map[string]string) (map[string]any, error) {
	if _, err := url.Parse(rawURL); err != nil {
		return nil, fmt.Errorf("invalid chaos endpoint URL: %w", err)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("chaos API error %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return map[string]any{"raw": string(respBody)}, nil
	}
	return result, nil
}

// DefaultExperiments returns starter chaos experiments for validation.
func DefaultExperiments() []Experiment {
	return []Experiment{
		{
			Name:     "pod-kill",
			Kind:     "PodChaos",
			Target:   "cloudripper",
			Duration: "30s",
			Parameters: map[string]any{
				"action": "pod-kill",
			},
		},
		{
			Name:     "network-delay",
			Kind:     "NetworkChaos",
			Target:   "cloudripper",
			Duration: "60s",
			Parameters: map[string]any{
				"action": "delay",
				"delay":  "500ms",
			},
		},
	}
}
