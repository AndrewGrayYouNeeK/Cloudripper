package scale

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Kind identifies what a policy scales.
const (
	KindASG = "asg"
	KindMIG = "mig"
	KindEC2 = "ec2"
	KindGCE = "gce"
)

// Action is the decision a policy produces.
const (
	ActionNone       = "none"
	ActionScaleOut   = "scale-out"
	ActionScaleIn    = "scale-in"
	ActionResizeUp   = "resize-up"
	ActionResizeDown = "resize-down"
)

// Policy is a horizontal or vertical scaling rule.
type Policy struct {
	Name              string        `json:"name"`
	Provider          string        `json:"provider"`
	Kind              string        `json:"kind"`
	Target            string        `json:"target"`
	Min               *int          `json:"min,omitempty"`
	Max               *int          `json:"max,omitempty"`
	Metric            string        `json:"metric,omitempty"`
	ScaleOutThreshold float64       `json:"scaleOutThreshold,omitempty"`
	ScaleInThreshold  float64       `json:"scaleInThreshold,omitempty"`
	Step              int           `json:"step,omitempty"`
	Cooldown          time.Duration `json:"-"`
	CooldownText      string        `json:"cooldown,omitempty"`
}

// File is the on-disk policy document.
type File struct {
	Policies []Policy `json:"policies"`
}

// Group is a scalable target plus its current capacity.
type Group struct {
	Provider string
	Kind     string
	Name     string
	Region   string
	Desired  int
	Min      int
	Max      int
	Spec     string
}

// Decision is the result of evaluating one policy against one observation.
type Decision struct {
	Action  string
	From    int
	To      int
	NewSpec string
	Reason  string
}

// Normalize fills defaults and validates a policy.
func (p *Policy) Normalize() error {
	if p.Provider == "" || p.Kind == "" || p.Target == "" {
		return fmt.Errorf("policy %q requires provider, kind, and target", p.Name)
	}
	if p.Metric == "" {
		p.Metric = "cpu"
	}
	if p.Metric != "cpu" {
		return fmt.Errorf("policy %q metric %q is unsupported (only cpu)", p.Name, p.Metric)
	}
	if p.ScaleOutThreshold == 0 && p.ScaleInThreshold == 0 {
		p.ScaleOutThreshold = 70
		p.ScaleInThreshold = 30
	}
	if p.ScaleOutThreshold <= p.ScaleInThreshold {
		return fmt.Errorf("policy %q scale-out threshold must be greater than scale-in", p.Name)
	}
	if p.Step <= 0 {
		p.Step = 1
	}
	if p.Cooldown == 0 {
		if p.CooldownText == "" {
			p.Cooldown = 5 * time.Minute
		} else {
			d, err := time.ParseDuration(p.CooldownText)
			if err != nil {
				return fmt.Errorf("policy %q cooldown: %w", p.Name, err)
			}
			p.Cooldown = d
		}
	}
	if p.Min != nil && p.Max != nil && *p.Max < *p.Min {
		return fmt.Errorf("policy %q max is below min", p.Name)
	}
	return nil
}

// LoadFile reads and normalizes a policy document.
func LoadFile(path string) ([]Policy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var file File
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("policy file: %w", err)
	}
	if len(file.Policies) == 0 {
		return nil, fmt.Errorf("policy file contains no policies")
	}
	for i := range file.Policies {
		if err := file.Policies[i].Normalize(); err != nil {
			return nil, err
		}
	}
	return file.Policies, nil
}

// Decide compares CPU utilization with policy thresholds.
// Horizontal kinds change desired capacity. ec2 and gce change the machine size
// and leave capacity at 1. lastScale zero means there is no cooldown yet.
func Decide(p Policy, g Group, cpu float64, lastScale, now time.Time) (Decision, error) {
	if err := p.Normalize(); err != nil {
		return Decision{}, err
	}
	if !lastScale.IsZero() && now.Sub(lastScale) < p.Cooldown {
		return Decision{Action: ActionNone, From: g.Desired, To: g.Desired, Reason: "cooldown"}, nil
	}
	min, max := bounds(p, g)
	switch p.Kind {
	case KindEC2, KindGCE:
		return decideVertical(p, g, cpu)
	case KindASG, KindMIG:
		return decideHorizontal(p, g, cpu, min, max)
	default:
		return Decision{}, fmt.Errorf("unsupported scale kind %q", p.Kind)
	}
}

func bounds(p Policy, g Group) (int, int) {
	min := g.Min
	max := g.Max
	if p.Min != nil {
		min = *p.Min
	}
	if p.Max != nil {
		max = *p.Max
	}
	if max < min {
		max = min
	}
	return min, max
}

func decideHorizontal(p Policy, g Group, cpu float64, min, max int) (Decision, error) {
	if cpu >= p.ScaleOutThreshold && g.Desired < max {
		to := g.Desired + p.Step
		if to > max {
			to = max
		}
		return Decision{
			Action: ActionScaleOut,
			From:   g.Desired,
			To:     to,
			Reason: fmt.Sprintf("cpu %.1f >= %.1f", cpu, p.ScaleOutThreshold),
		}, nil
	}
	if cpu <= p.ScaleInThreshold && g.Desired > min {
		to := g.Desired - p.Step
		if to < min {
			to = min
		}
		return Decision{
			Action: ActionScaleIn,
			From:   g.Desired,
			To:     to,
			Reason: fmt.Sprintf("cpu %.1f <= %.1f", cpu, p.ScaleInThreshold),
		}, nil
	}
	return Decision{Action: ActionNone, From: g.Desired, To: g.Desired, Reason: "within thresholds"}, nil
}

func decideVertical(p Policy, g Group, cpu float64) (Decision, error) {
	if cpu >= p.ScaleOutThreshold {
		return Decision{
			Action: ActionResizeUp,
			From:   1,
			To:     1,
			Reason: fmt.Sprintf("cpu %.1f >= %.1f", cpu, p.ScaleOutThreshold),
		}, nil
	}
	if cpu <= p.ScaleInThreshold {
		return Decision{
			Action: ActionResizeDown,
			From:   1,
			To:     1,
			Reason: fmt.Sprintf("cpu %.1f <= %.1f", cpu, p.ScaleInThreshold),
		}, nil
	}
	return Decision{Action: ActionNone, From: 1, To: 1, Reason: "within thresholds"}, nil
}

// DefaultHorizontal builds a policy from a discovered group's own min and max.
func DefaultHorizontal(g Group) (Policy, error) {
	min := g.Min
	max := g.Max
	p := Policy{
		Name:     g.Provider + "-" + g.Name,
		Provider: g.Provider,
		Kind:     g.Kind,
		Target:   g.Name,
		Min:      &min,
		Max:      &max,
	}
	if err := p.Normalize(); err != nil {
		return Policy{}, err
	}
	return p, nil
}

// CooldownKey identifies a target in the scale state file.
func CooldownKey(provider, kind, name string) string {
	return provider + "/" + kind + "/" + name
}
