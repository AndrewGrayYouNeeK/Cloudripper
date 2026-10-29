package cost

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/cloud"
)

// ServiceCost is account spend for one cloud service over the query window.
type ServiceCost struct {
	Service string  `json:"service"`
	USD     float64 `json:"usd"`
}

// ResourceCost is account spend attributed to one resource ID.
type ResourceCost struct {
	ResourceID string  `json:"resource_id"`
	Service    string  `json:"service,omitempty"`
	USD        float64 `json:"usd"`
}

// Snapshot is one billing source over a trailing window (normally 30 days).
type Snapshot struct {
	Source    string         `json:"source"`
	TotalUSD  float64        `json:"total_usd"`
	Services  []ServiceCost  `json:"services,omitempty"`
	Resources []ResourceCost `json:"resources,omitempty"`
	Warnings  []string       `json:"warnings,omitempty"`
}

// Source fetches actual spend. A Fetch error means the caller should keep estimates.
type Source interface {
	Name() string
	Fetch(ctx context.Context) (Snapshot, error)
}

// Resolution is the merged view of estimates and actuals.
type Resolution struct {
	Resources       []cloud.Resource `json:"resources"`
	Snapshots       []Snapshot       `json:"snapshots,omitempty"`
	Warnings        []string         `json:"warnings,omitempty"`
	EstimatedTotal  float64          `json:"estimated_total"`
	ActualTotal     float64          `json:"actual_total"`
	ActualAvailable bool             `json:"actual_available"`
}

// Resolve overlays actual resource costs onto a copy of resources.
// Sources that fail are recorded as warnings and do not discard estimates.
func Resolve(ctx context.Context, resources []cloud.Resource, sources []Source) Resolution {
	res := Resolution{
		Resources:      cloneResources(resources),
		EstimatedTotal: sumCost(resources),
	}
	for _, src := range sources {
		if src == nil {
			continue
		}
		snap, err := src.Fetch(ctx)
		if err != nil {
			res.Warnings = appendWarning(res.Warnings, fmt.Sprintf("%s: %v", src.Name(), err))
			continue
		}
		if snap.Source == "" {
			snap.Source = src.Name()
		}
		res.Snapshots = append(res.Snapshots, snap)
		for _, warning := range snap.Warnings {
			res.Warnings = appendWarning(res.Warnings, warning)
		}
		res.ActualTotal += snap.TotalUSD
		res.ActualAvailable = true
		applySnapshot(&res, snap)
	}
	return res
}

func cloneResources(in []cloud.Resource) []cloud.Resource {
	out := make([]cloud.Resource, len(in))
	copy(out, in)
	return out
}

func sumCost(resources []cloud.Resource) float64 {
	var total float64
	for _, r := range resources {
		total += r.CostUSD
	}
	return total
}

func applySnapshot(res *Resolution, snap Snapshot) {
	if len(snap.Resources) == 0 {
		return
	}
	provider := providerForSource(snap.Source)
	for i := range res.Resources {
		r := &res.Resources[i]
		if provider != "" && r.Provider != provider {
			continue
		}
		var matched float64
		var hit bool
		for _, row := range snap.Resources {
			if cloud.SameResource(r.ID, row.ResourceID) || cloud.SameResource(r.Name, row.ResourceID) {
				matched += row.USD
				hit = true
			}
		}
		if !hit {
			continue
		}
		r.CostUSD = matched
		r.CostSource = snap.Source
	}
}

func providerForSource(source string) string {
	switch source {
	case cloud.CostSourceAWS, "aws":
		return "aws"
	case cloud.CostSourceGCP, "gcp":
		return "gcp"
	case cloud.CostSourceAzure, "azure":
		return "azure"
	default:
		return ""
	}
}

// Summary renders actuals versus the estimate fallback for humans.
func Summary(res Resolution) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Estimated resource total: $%.2f\n", res.EstimatedTotal)
	if !res.ActualAvailable {
		b.WriteString("Actual spend: unavailable, using estimates\n")
	} else {
		fmt.Fprintf(&b, "Actual spend (trailing window): $%.2f\n", res.ActualTotal)
		for _, snap := range res.Snapshots {
			fmt.Fprintf(&b, "  %s: $%.2f across %d services", snap.Source, snap.TotalUSD, len(snap.Services))
			if len(snap.Resources) > 0 {
				fmt.Fprintf(&b, ", %d resources", len(snap.Resources))
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

func appendWarning(dst []string, warning string) []string {
	warning = strings.TrimSpace(warning)
	if warning == "" {
		return dst
	}
	for _, existing := range dst {
		if existing == warning {
			return dst
		}
	}
	return append(dst, warning)
}

// Window is the trailing 30 days ending today (end exclusive, date precision).
func Window(now time.Time) (start, end time.Time) {
	now = now.UTC()
	end = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
	start = end.AddDate(0, 0, -30)
	return start, end
}

// ParseUSD parses a billing amount. Non-USD units return an error.
func ParseUSD(amount, unit string) (float64, error) {
	if unit != "" && !strings.EqualFold(unit, "USD") {
		return 0, fmt.Errorf("unsupported currency %q", unit)
	}
	var v float64
	if _, err := fmt.Sscan(amount, &v); err != nil {
		return 0, fmt.Errorf("amount %q: %w", amount, err)
	}
	return v, nil
}
