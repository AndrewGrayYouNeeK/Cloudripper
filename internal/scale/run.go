package scale

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/audit"
)

// Controller discovers scalable targets and applies a decision.
type Controller interface {
	Provider() string
	Discover(ctx context.Context) ([]Group, error)
	Lookup(ctx context.Context, kind, name string) (Group, error)
	CPU(ctx context.Context, g Group) (float64, error)
	Apply(ctx context.Context, g Group, decision Decision) error
}

// Confirmer gates scaling changes.
type Confirmer interface {
	Confirm(summary string) (bool, error)
}

// RunOptions controls one scale pass.
type RunOptions struct {
	Policies  []Policy
	DryRun    bool
	Confirmer Confirmer
	StatePath string
	Now       func() time.Time
	Audit     *audit.Logger
	Command   string
}

// Result is one policy evaluation.
type Result struct {
	Provider string  `json:"provider"`
	Kind     string  `json:"kind"`
	Target   string  `json:"target"`
	Action   string  `json:"action"`
	From     int     `json:"from"`
	To       int     `json:"to"`
	NewSpec  string  `json:"new_spec,omitempty"`
	CPU      float64 `json:"cpu"`
	Outcome  string  `json:"outcome"`
	Detail   string  `json:"detail,omitempty"`
}

type cooldownState struct {
	Last map[string]time.Time `json:"last"`
}

// Run evaluates policies and, unless dry-run, confirms once before applying.
func Run(ctx context.Context, controllers []Controller, opts RunOptions) ([]Result, error) {
	now := time.Now().UTC()
	if opts.Now != nil {
		now = opts.Now().UTC()
	}
	policies := opts.Policies
	discovered := map[string]Group{}
	if len(policies) == 0 {
		var err error
		policies, discovered, err = defaults(ctx, controllers)
		if err != nil {
			return nil, err
		}
		if len(policies) == 0 {
			return nil, fmt.Errorf("no auto scaling groups or managed instance groups found")
		}
	}
	state := loadCooldown(opts.StatePath)
	var pending []Result
	var results []Result
	for _, policy := range policies {
		ctrl := findController(controllers, policy.Provider)
		if ctrl == nil {
			results = append(results, Result{Provider: policy.Provider, Kind: policy.Kind, Target: policy.Target, Outcome: "failed", Detail: "no controller"})
			continue
		}
		group, ok := discovered[CooldownKey(policy.Provider, policy.Kind, policy.Target)]
		if !ok {
			var err error
			group, err = ctrl.Lookup(ctx, policy.Kind, policy.Target)
			if err != nil {
				results = append(results, Result{Provider: policy.Provider, Kind: policy.Kind, Target: policy.Target, Outcome: "failed", Detail: err.Error()})
				continue
			}
		}
		cpu, err := ctrl.CPU(ctx, group)
		if err != nil {
			results = append(results, Result{Provider: group.Provider, Kind: group.Kind, Target: group.Name, Outcome: "failed", Detail: "metrics: " + err.Error()})
			continue
		}
		decision, err := Decide(policy, group, cpu, state.Last[CooldownKey(group.Provider, group.Kind, group.Name)], now)
		if err != nil {
			results = append(results, Result{Provider: group.Provider, Kind: group.Kind, Target: group.Name, CPU: cpu, Outcome: "failed", Detail: err.Error()})
			continue
		}
		item := Result{
			Provider: group.Provider,
			Kind:     group.Kind,
			Target:   group.Name,
			Action:   decision.Action,
			From:     decision.From,
			To:       decision.To,
			NewSpec:  decision.NewSpec,
			CPU:      cpu,
			Detail:   decision.Reason,
		}
		if decision.Action == ActionNone {
			item.Outcome = "none"
			results = append(results, item)
			continue
		}
		if opts.DryRun {
			item.Outcome = "dry-run"
			results = append(results, item)
			continue
		}
		pending = append(pending, item)
	}
	if len(pending) == 0 || opts.DryRun {
		writeScaleAudit(opts, results)
		return results, nil
	}
	if opts.Confirmer == nil {
		return nil, fmt.Errorf("refusing to scale without confirmation (--yes or an interactive terminal)")
	}
	ok, err := opts.Confirmer.Confirm(scaleSummary(pending))
	if err != nil {
		return nil, err
	}
	if !ok {
		for i := range pending {
			pending[i].Outcome = "skipped-confirm"
		}
		results = append(results, pending...)
		writeScaleAudit(opts, results)
		return results, nil
	}
	for i := range pending {
		ctrl := findController(controllers, pending[i].Provider)
		group := Group{Provider: pending[i].Provider, Kind: pending[i].Kind, Name: pending[i].Target, Desired: pending[i].From}
		if looked, err := ctrl.Lookup(ctx, pending[i].Kind, pending[i].Target); err == nil {
			group = looked
		}
		decision := Decision{Action: pending[i].Action, From: pending[i].From, To: pending[i].To, NewSpec: pending[i].NewSpec, Reason: pending[i].Detail}
		if err := ctrl.Apply(ctx, group, decision); err != nil {
			pending[i].Outcome = "failed"
			pending[i].Detail = err.Error()
			continue
		}
		pending[i].Outcome = "applied"
		if state.Last == nil {
			state.Last = map[string]time.Time{}
		}
		state.Last[CooldownKey(pending[i].Provider, pending[i].Kind, pending[i].Target)] = now
	}
	if err := saveCooldown(opts.StatePath, state); err != nil {
		return append(results, pending...), err
	}
	results = append(results, pending...)
	writeScaleAudit(opts, results)
	return results, nil
}

func defaults(ctx context.Context, controllers []Controller) ([]Policy, map[string]Group, error) {
	var policies []Policy
	found := map[string]Group{}
	var errs []string
	for _, ctrl := range controllers {
		groups, err := ctrl.Discover(ctx)
		if err != nil {
			errs = append(errs, ctrl.Provider()+": "+err.Error())
			continue
		}
		for _, g := range groups {
			if g.Kind != KindASG && g.Kind != KindMIG {
				continue
			}
			p, err := DefaultHorizontal(g)
			if err != nil {
				continue
			}
			policies = append(policies, p)
			found[CooldownKey(g.Provider, g.Kind, g.Name)] = g
		}
	}
	if len(policies) == 0 && len(errs) > 0 {
		return nil, nil, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return policies, found, nil
}

func findController(controllers []Controller, provider string) Controller {
	for _, c := range controllers {
		if c != nil && c.Provider() == provider {
			return c
		}
	}
	return nil
}

func scaleSummary(items []Result) string {
	var b strings.Builder
	b.WriteString("CloudRipper will scale:\n")
	for i, item := range items {
		fmt.Fprintf(&b, "  %d. %s %s/%s %s", i+1, item.Action, item.Provider, item.Target, item.Detail)
		if item.To != item.From {
			fmt.Fprintf(&b, " (%d -> %d)", item.From, item.To)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func loadCooldown(path string) cooldownState {
	state := cooldownState{Last: map[string]time.Time{}}
	if path == "" {
		return state
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return state
	}
	_ = json.Unmarshal(data, &state)
	if state.Last == nil {
		state.Last = map[string]time.Time{}
	}
	return state
}

func saveCooldown(path string, state cooldownState) error {
	if path == "" {
		return nil
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func writeScaleAudit(opts RunOptions, results []Result) {
	if opts.Audit == nil {
		return
	}
	for _, r := range results {
		_ = opts.Audit.Write(audit.Event{
			Command:    opts.Command,
			Action:     r.Action,
			Provider:   r.Provider,
			ResourceID: r.Target,
			Outcome:    r.Outcome,
			Detail:     r.Detail,
			DryRun:     opts.DryRun || r.Outcome == "dry-run",
		})
	}
}

// Average returns the mean of metric samples.
func Average(values []float64) (float64, error) {
	if len(values) == 0 {
		return 0, fmt.Errorf("no datapoints")
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values)), nil
}

// PercentFromFraction converts a 0..1 utilization ratio to a percent.
func PercentFromFraction(fraction float64) float64 {
	return fraction * 100
}
