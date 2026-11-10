package cost

import (
	"context"
	"fmt"
	"math"
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
	UnpricedCount   int              `json:"unpriced_count,omitempty"`
	ActualTotal     float64          `json:"actual_total"`
	ActualAvailable bool             `json:"actual_available"`
}

// Resolve overlays actual resource costs onto a copy of resources.
// Sources that fail are recorded as warnings and do not discard estimates.
func Resolve(ctx context.Context, resources []cloud.Resource, sources []Source) Resolution {
	res := Resolution{
		Resources:      cloneResources(resources),
		EstimatedTotal: sumCost(resources),
		UnpricedCount:  countUnpriced(resources),
	}
	if res.UnpricedCount > 0 {
		res.Warnings = appendWarning(res.Warnings, fmt.Sprintf(
			"estimated_total excludes %d unpriced resource(s); per-resource cost is unavailable for those rows (not a $0 estimate)",
			res.UnpricedCount,
		))
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
		if !r.CostPriced() {
			continue
		}
		total += r.CostUSD
	}
	return total
}

func countUnpriced(resources []cloud.Resource) int {
	var n int
	for _, r := range resources {
		if !r.CostPriced() {
			n++
		}
	}
	return n
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
	case cloud.CostSourceCloudflareBilling, "cloudflare":
		return "cloudflare"
	default:
		return ""
	}
}

// Summary renders actuals versus the estimate fallback for humans.
func Summary(res Resolution) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Estimated resource total: $%.2f", res.EstimatedTotal)
	if res.UnpricedCount > 0 {
		fmt.Fprintf(&b, " (%d unpriced excluded; coverage incomplete)", res.UnpricedCount)
	}
	b.WriteString("\n")
	if !res.ActualAvailable {
		if res.UnpricedCount > 0 {
			b.WriteString("Actual spend: unavailable; unpriced rows are not treated as $0\n")
		} else {
			b.WriteString("Actual spend: unavailable, using estimates\n")
		}
	} else {
		fmt.Fprintf(&b, "Actual spend (trailing window): $%.2f\n", noNegZero(res.ActualTotal))
		for _, snap := range res.Snapshots {
			serviceCount := len(snap.Services)
			serviceWord := "services"
			if serviceCount == 1 {
				serviceWord = "service"
			}
			fmt.Fprintf(&b, "  %s: $%.2f across %d %s", snap.Source, noNegZero(snap.TotalUSD), serviceCount, serviceWord)
			if len(snap.Resources) > 0 {
				fmt.Fprintf(&b, ", %d resources", len(snap.Resources))
			}
			if snap.Source == cloud.CostSourceCloudflareBilling {
				b.WriteString(" (account invoice/billing-history by occurred_at; not per-resource)")
			}
			b.WriteString("\n")
		}
		if res.UnpricedCount > 0 {
			fmt.Fprintf(&b, "Note: %d resource row(s) remain unpriced; estimate total covers only priced inventory\n", res.UnpricedCount)
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

// noNegZero avoids printing "$-0.00" for tiny credits or rounding noise.
func noNegZero(v float64) float64 {
	if v > -0.005 && v < 0.005 {
		return 0
	}
	return v
}

// RoundCents rounds a dollar amount to the nearest cent so JSON totals do not
// print binary float residue (for example 6.12 + -1e-10 encoding as 6.1199999999).
func RoundCents(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	rounded := math.Round(v*100) / 100
	if rounded == 0 {
		return 0
	}
	return rounded
}
