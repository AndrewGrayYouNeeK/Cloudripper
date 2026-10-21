package chaos

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SafetyPolicy bounds the blast radius of an experiment.
type SafetyPolicy struct {
	MaxDuration      time.Duration
	MaxTargets       int
	AllowProduction  bool
	DeniedNamespaces []string
}

// DefaultSafety is the built-in blast-radius policy.
func DefaultSafety() SafetyPolicy {
	return SafetyPolicy{
		MaxDuration:      5 * time.Minute,
		MaxTargets:       20,
		DeniedNamespaces: []string{"kube-system", "kube-public", "kube-node-lease"},
	}
}

// Validate rejects experiments that exceed the blast-radius policy.
func Validate(exp Experiment, policy SafetyPolicy) error {
	target := strings.TrimSpace(exp.Target)
	if target == "" || target == "*" || strings.EqualFold(target, "all") {
		return fmt.Errorf("target %q is too broad", exp.Target)
	}
	duration, err := time.ParseDuration(exp.Duration)
	if err != nil {
		return fmt.Errorf("duration: %w", err)
	}
	if duration <= 0 {
		return fmt.Errorf("duration must be positive")
	}
	if policy.MaxDuration > 0 && duration > policy.MaxDuration {
		return fmt.Errorf("duration %s exceeds blast-radius limit %s", duration, policy.MaxDuration)
	}
	ns := Namespace(exp)
	for _, denied := range policy.DeniedNamespaces {
		if strings.EqualFold(ns, denied) {
			return fmt.Errorf("namespace %s is protected", ns)
		}
	}
	if !policy.AllowProduction && looksLikeProduction(target, ns) {
		return fmt.Errorf("target %q looks like production; pass --allow-prod to override", target)
	}
	count := TargetCount(exp)
	if policy.MaxTargets > 0 && count > policy.MaxTargets {
		return fmt.Errorf("target count %d exceeds limit %d", count, policy.MaxTargets)
	}
	return nil
}

// Namespace reads an explicit namespace parameter or a namespace/name target.
func Namespace(exp Experiment) string {
	if exp.Parameters != nil {
		if v, ok := exp.Parameters["namespace"].(string); ok {
			return v
		}
	}
	if strings.Contains(exp.Target, "/") {
		return strings.SplitN(exp.Target, "/", 2)[0]
	}
	return ""
}

// TargetCount reads parameters["count"] and defaults to 1.
func TargetCount(exp Experiment) int {
	if exp.Parameters == nil {
		return 1
	}
	switch n := exp.Parameters["count"].(type) {
	case int:
		if n > 0 {
			return n
		}
	case float64:
		if n > 0 {
			return int(n)
		}
	}
	return 1
}

func looksLikeProduction(target, namespace string) bool {
	for _, value := range []string{target, namespace} {
		for _, part := range strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
			return r == '-' || r == '_' || r == '/' || r == '.'
		}) {
			switch part {
			case "prod", "production", "prd":
				return true
			}
		}
	}
	return false
}

// Status is a normalized experiment phase.
type Status struct {
	Phase string
	Raw   map[string]any
}

// NormalizeStatus maps provider payloads onto pending, running, finished, or failed.
func NormalizeStatus(provider Provider, body map[string]any) Status {
	if body == nil {
		return Status{Phase: "unknown"}
	}
	phase := strings.ToLower(firstString(body, "phase", "state", "status"))
	switch provider {
	case ProviderGremlin:
		switch phase {
		case "running", "attacking":
			phase = "running"
		case "halted", "finished", "completed":
			phase = "finished"
		case "failed", "error":
			phase = "failed"
		case "":
			phase = "unknown"
		default:
			phase = "pending"
		}
	default:
		if recovered, ok := nestedBool(body, "status", "conditions"); ok && recovered {
			phase = "finished"
		}
		switch phase {
		case "running", "injecting":
			phase = "running"
		case "finished", "recovered", "done", "succeeded":
			phase = "finished"
		case "failed", "error":
			phase = "failed"
		case "":
			if p := nestedString(body, "status", "phase"); p != "" {
				return NormalizeStatus(provider, map[string]any{"phase": p})
			}
			phase = "unknown"
		default:
			if phase != "pending" {
				phase = "pending"
			}
		}
	}
	return Status{Phase: phase, Raw: body}
}

func firstString(body map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := body[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func nestedString(body map[string]any, keys ...string) string {
	var cur any = body
	for _, key := range keys {
		m, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = m[key]
	}
	s, _ := cur.(string)
	return s
}

func nestedBool(body map[string]any, statusKey, conditionsKey string) (bool, bool) {
	status, ok := body[statusKey].(map[string]any)
	if !ok {
		return false, false
	}
	conditions, ok := status[conditionsKey].([]any)
	if !ok {
		return false, false
	}
	for _, item := range conditions {
		cond, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if cond["type"] == "AllRecovered" && strings.EqualFold(fmt.Sprint(cond["status"]), "True") {
			return true, true
		}
	}
	return false, false
}

// SetHTTPClient overrides the HTTP client (tests).
func (o *Orchestrator) SetHTTPClient(client *http.Client) {
	if client != nil {
		o.client = client
	}
}

// Abort deletes a Chaos Mesh experiment or halts a Gremlin attack.
func (o *Orchestrator) Abort(ctx context.Context, ref string) (map[string]any, error) {
	if strings.TrimSpace(ref) == "" {
		return nil, fmt.Errorf("experiment reference is required")
	}
	endpoint, err := o.resourceURL(ref)
	if err != nil {
		return nil, err
	}
	return o.do(ctx, http.MethodDelete, endpoint, nil, o.authHeaders())
}

// Status fetches one experiment.
func (o *Orchestrator) Status(ctx context.Context, ref string) (Status, error) {
	endpoint, err := o.resourceURL(ref)
	if err != nil {
		return Status{}, err
	}
	body, err := o.do(ctx, http.MethodGet, endpoint, nil, o.authHeaders())
	if err != nil {
		return Status{}, err
	}
	return NormalizeStatus(o.config.Provider, body), nil
}

// Poll waits until the experiment finishes, fails, or the timeout elapses.
func (o *Orchestrator) Poll(ctx context.Context, ref string, interval, timeout time.Duration) (Status, error) {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	deadline := time.Now().Add(timeout)
	var last Status
	for {
		status, err := o.Status(ctx, ref)
		if err != nil {
			return last, err
		}
		last = status
		if status.Phase == "finished" || status.Phase == "failed" {
			return status, nil
		}
		if time.Now().After(deadline) {
			return status, fmt.Errorf("timed out waiting for experiment %s (phase %s)", ref, status.Phase)
		}
		select {
		case <-ctx.Done():
			return status, ctx.Err()
		case <-time.After(interval):
		}
	}
}

// Rollback aborts an experiment. It is the supported undo for chaos injection.
func (o *Orchestrator) Rollback(ctx context.Context, ref string) error {
	_, err := o.Abort(ctx, ref)
	return err
}

func (o *Orchestrator) resourceURL(ref string) (string, error) {
	ref = url.PathEscape(ref)
	switch o.config.Provider {
	case ProviderChaosMesh:
		return buildEndpointURL(o.config.Endpoint, "/api/v1/experiments/"+ref, "http"), nil
	case ProviderGremlin:
		return buildEndpointURL(o.config.Endpoint, "/v1/attacks/"+ref, "https"), nil
	default:
		return "", fmt.Errorf("unknown chaos provider: %s", o.config.Provider)
	}
}

func (o *Orchestrator) authHeaders() map[string]string {
	if o.config.Provider == ProviderGremlin || o.config.APIKey != "" {
		return map[string]string{"Authorization": "Bearer " + o.config.APIKey}
	}
	return nil
}

// ExperimentRef chooses the identifier used for status and abort.
func ExperimentRef(provider Provider, exp Experiment, resp map[string]any) string {
	if resp != nil {
		if id := firstString(resp, "id", "guid", "uuid"); id != "" {
			return id
		}
		if meta, ok := resp["metadata"].(map[string]any); ok {
			if name, ok := meta["name"].(string); ok && name != "" {
				return name
			}
		}
	}
	if exp.Name != "" {
		return exp.Name
	}
	return ""
}
