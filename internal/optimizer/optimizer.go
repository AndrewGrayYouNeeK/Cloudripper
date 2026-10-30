package optimizer

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/cloud"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/schedule"
)

// ActionType describes an optimization recommendation.
type ActionType string

const (
	ActionRightsize   ActionType = "rightsize"
	ActionTerminate   ActionType = "terminate"
	ActionSchedule    ActionType = "schedule"
	ActionConsolidate ActionType = "consolidate"
	ActionStop        ActionType = "stop"
	ActionStart       ActionType = "start"
)

// Recommendation is a cost-saving suggestion for a resource.
type Recommendation struct {
	Resource      cloud.Resource
	Action        ActionType
	Savings       float64
	Reason        string
	SuggestedSpec string
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
	if cloud.IsProtected(r.Tags) {
		return Recommendation{}, false
	}
	status := strings.ToLower(r.Status)

	switch r.Type {
	case cloud.ResourceRDS:
		return analyzeDatabase(r, status)
	case cloud.ResourceEBS, cloud.ResourceGCPDisk, cloud.ResourceAzureDisk:
		if isUnattachedDisk(status) && r.CostUSD > 0 {
			return Recommendation{
				Resource: r,
				Action:   ActionTerminate,
				Savings:  r.CostUSD,
				Reason:   "unattached disk is still billed",
			}, true
		}
		return Recommendation{}, false
	case cloud.ResourceEIP, cloud.ResourceGCPIP, cloud.ResourceAzureIP:
		if isUnusedAddress(status) {
			return Recommendation{
				Resource: r,
				Action:   ActionTerminate,
				Savings:  r.CostUSD,
				Reason:   "unattached public IP is billed while idle",
			}, true
		}
		return Recommendation{}, false
	case cloud.ResourceELB, cloud.ResourceGCPLB, cloud.ResourceAzureLB:
		if status == "idle" && r.CostUSD > 0 {
			return Recommendation{
				Resource: r,
				Action:   ActionTerminate,
				Savings:  r.CostUSD,
				Reason:   "load balancer has no registered backends",
			}, true
		}
		return Recommendation{}, false
	}

	if isStopped(status) {
		if !cloud.IsCompute(r.Type) || r.CostUSD < 1 {
			return Recommendation{}, false
		}
		return Recommendation{
			Resource: r,
			Action:   ActionTerminate,
			Savings:  r.CostUSD,
			Reason:   "stopped instance still incurring attached storage costs",
		}, true
	}

	if status == "terminated" || status == "shutting-down" {
		return Recommendation{}, false
	}

	if r.CostUSD >= 100 && cloud.IsCompute(r.Type) {
		spec, _ := cloud.SmallerSpec(r.Provider, r.Spec)
		return Recommendation{
			Resource:      r,
			Action:        ActionRightsize,
			Savings:       r.CostUSD * 0.35,
			Reason:        "high-cost instance — consider downsizing or reserved pricing",
			SuggestedSpec: spec,
		}, true
	}

	if isRunning(status) && cloud.IsCompute(r.Type) {
		if _, managed := r.Tags["cloudripper:managed"]; !managed {
			return Recommendation{
				Resource:      r,
				Action:        ActionSchedule,
				Savings:       r.CostUSD * 0.5,
				Reason:        "unmanaged instance — schedule off-hours shutdown",
				SuggestedSpec: schedule.DefaultOffWindow,
			}, true
		}
	}

	return Recommendation{}, false
}

func analyzeDatabase(r cloud.Resource, status string) (Recommendation, bool) {
	if status == "available" && r.CostUSD >= 100 {
		spec, _ := cloud.SmallerSpec(r.Provider, r.Spec)
		return Recommendation{
			Resource:      r,
			Action:        ActionRightsize,
			Savings:       r.CostUSD * 0.35,
			Reason:        "high-cost database — consider a smaller instance class",
			SuggestedSpec: spec,
		}, true
	}
	return Recommendation{}, false
}

func isStopped(status string) bool {
	switch status {
	case "stopped", "suspended", "deallocated":
		return true
	default:
		return false
	}
}

func isRunning(status string) bool {
	switch status {
	case "running", "active":
		return true
	default:
		return false
	}
}

func isUnattachedDisk(status string) bool {
	switch status {
	case "available", "unattached":
		return true
	default:
		return false
	}
}

func isUnusedAddress(status string) bool {
	switch status {
	case "unattached", "reserved", "unused":
		return true
	default:
		return false
	}
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
		reason := rec.Reason
		if rec.SuggestedSpec != "" {
			reason = fmt.Sprintf("%s (proposed: %s)", reason, rec.SuggestedSpec)
		}
		fmt.Fprintf(&b, "%d. [%s] %s/%s (%s) — save $%.2f/mo\n   → %s\n",
			i+1,
			rec.Action,
			rec.Resource.Provider,
			rec.Resource.DisplayName(),
			rec.Resource.ID,
			rec.Savings,
			reason,
		)
	}
	return b.String()
}

// PlanSchedules turns schedule tags into stop/start actions for the current time.
func PlanSchedules(resources []cloud.Resource, now time.Time) []Recommendation {
	var recs []Recommendation
	for _, r := range resources {
		if !cloud.IsCompute(r.Type) || cloud.IsProtected(r.Tags) {
			continue
		}
		spec := ""
		for k, v := range r.Tags {
			if strings.EqualFold(k, schedule.Tag) {
				spec = v
			}
		}
		if spec == "" {
			continue
		}
		off, err := schedule.InOffWindow(spec, now)
		if err != nil {
			continue
		}
		status := strings.ToLower(r.Status)
		if off && (status == "running" || status == "active") {
			recs = append(recs, Recommendation{
				Resource: r,
				Action:   ActionStop,
				Reason:   "schedule " + spec + " is in the off window",
			})
		}
		if !off && (status == "stopped" || status == "suspended" || status == "deallocated") {
			recs = append(recs, Recommendation{
				Resource: r,
				Action:   ActionStart,
				Reason:   "schedule " + spec + " is in the on window",
			})
		}
	}
	return recs
}

// WithImmediateSchedule adds a stop action when a new schedule is already inside its off window.
func WithImmediateSchedule(recs []Recommendation, now time.Time) []Recommendation {
	var out []Recommendation
	for _, rec := range recs {
		out = append(out, rec)
		if rec.Action != ActionSchedule {
			continue
		}
		spec := rec.SuggestedSpec
		if spec == "" {
			spec = schedule.DefaultOffWindow
		}
		off, err := schedule.InOffWindow(spec, now)
		if err != nil || !off {
			continue
		}
		status := strings.ToLower(rec.Resource.Status)
		if status == "running" || status == "active" {
			out = append(out, Recommendation{
				Resource: rec.Resource,
				Action:   ActionStop,
				Reason:   "new schedule is already in the off window",
			})
		}
	}
	return out
}

func percent(part, total float64) float64 {
	if total == 0 {
		return 0
	}
	return (part / total) * 100
}
