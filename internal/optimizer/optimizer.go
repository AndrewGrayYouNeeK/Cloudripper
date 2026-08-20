package optimizer

import (
	"fmt"
	"sort"
	"strings"

	"github.com/AndrewGrayYouNeeK/cloudripper/internal/cloud"
)

// ActionType describes an optimization recommendation.
type ActionType string

const (
	ActionRightsize  ActionType = "rightsize"
	ActionTerminate  ActionType = "terminate"
	ActionSchedule   ActionType = "schedule"
	ActionConsolidate ActionType = "consolidate"
)

// Recommendation is a cost-saving suggestion for a resource.
type Recommendation struct {
	Resource cloud.Resource
	Action   ActionType
	Savings  float64
	Reason   string
}

// Result summarizes optimization analysis.
type Result struct {
	TotalMonthlyCost float64
	PotentialSavings float64
	Recommendations  []Recommendation
}

// Analyze inspects resources and returns cost optimization recommendations.
func Analyze(resources []cloud.Resource) Result {
	var result Result
	for _, r := range resources {
		result.TotalMonthlyCost += r.CostUSD
		if rec, ok := analyzeResource(r); ok {
			result.Recommendations = append(result.Recommendations, rec)
			result.PotentialSavings += rec.Savings
		}
	}

	sort.Slice(result.Recommendations, func(i, j int) bool {
		return result.Recommendations[i].Savings > result.Recommendations[j].Savings
	})

	return result
}

func analyzeResource(r cloud.Resource) (Recommendation, bool) {
	status := strings.ToLower(r.Status)

	if status == "stopped" {
		// Stopped EC2 only incurs storage costs; recommend cleanup if material.
		if r.CostUSD < 1 {
			return Recommendation{}, false
		}
		return Recommendation{
			Resource: r,
			Action:   ActionTerminate,
			Savings:  r.CostUSD,
			Reason:   "stopped instance still incurring attached storage costs",
		}, true
	}

	if status == "terminated" {
		return Recommendation{}, false
	}

	if r.CostUSD >= 100 {
		return Recommendation{
			Resource: r,
			Action:   ActionRightsize,
			Savings:  r.CostUSD * 0.35,
			Reason:   "high-cost instance — consider downsizing or reserved pricing",
		}, true
	}

	if status == "running" && r.Type == cloud.ResourceEC2 {
		if _, managed := r.Tags["cloudripper:managed"]; !managed {
			return Recommendation{
				Resource: r,
				Action:   ActionSchedule,
				Savings:  r.CostUSD * 0.5,
				Reason:   "unmanaged instance — schedule off-hours shutdown",
			}, true
		}
	}

	return Recommendation{}, false
}

// FormatReport renders a human-readable optimization report.
func FormatReport(result Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Monthly spend: $%.2f\n", result.TotalMonthlyCost)
	fmt.Fprintf(&b, "Potential savings: $%.2f (%.0f%%)\n\n",
		result.PotentialSavings,
		percent(result.PotentialSavings, result.TotalMonthlyCost),
	)

	if len(result.Recommendations) == 0 {
		b.WriteString("No optimization opportunities found. Your infra is already ripped tight.\n")
		return b.String()
	}

	b.WriteString("Recommendations:\n")
	for i, rec := range result.Recommendations {
		fmt.Fprintf(&b, "%d. [%s] %s/%s (%s) — save $%.2f/mo\n   → %s\n",
			i+1,
			rec.Action,
			rec.Resource.Provider,
			rec.Resource.Name,
			rec.Resource.ID,
			rec.Savings,
			rec.Reason,
		)
	}
	return b.String()
}

func percent(part, total float64) float64 {
	if total == 0 {
		return 0
	}
	return (part / total) * 100
}
