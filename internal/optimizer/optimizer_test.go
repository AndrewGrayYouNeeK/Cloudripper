package optimizer

import (
	"testing"

	"github.com/AndrewGrayYouNeeK/cloudripper/internal/cloud"
)

func TestAnalyzeStoppedInstance(t *testing.T) {
	running := 75.0
	storage := running * 0.1
	resources := []cloud.Resource{
		{
			ID:       "i-123",
			Name:     "old-server",
			Provider: "aws",
			Type:     cloud.ResourceEC2,
			Status:   "stopped",
			CostUSD:  storage,
		},
	}

	result := Analyze(resources)
	if result.TotalMonthlyCost != storage {
		t.Fatalf("expected total spend %f, got %f", storage, result.TotalMonthlyCost)
	}
	if len(result.Recommendations) != 1 {
		t.Fatalf("expected 1 recommendation, got %d", len(result.Recommendations))
	}
	rec := result.Recommendations[0]
	if rec.Action != ActionTerminate {
		t.Errorf("expected terminate action, got %s", rec.Action)
	}
	if rec.Savings != storage {
		t.Fatalf("savings should match storage cost %f, got %f", storage, rec.Savings)
	}
	if rec.Savings >= running {
		t.Fatal("savings must not use full running price for stopped instance")
	}
}

func TestAnalyzeTerminatedInstanceExcluded(t *testing.T) {
	resources := []cloud.Resource{
		{
			ID:       "i-dead",
			Provider: "aws",
			Type:     cloud.ResourceEC2,
			Status:   "terminated",
			CostUSD:  0,
		},
	}

	result := Analyze(resources)
	if result.TotalMonthlyCost != 0 {
		t.Fatalf("terminated instance should not contribute to spend, got %f", result.TotalMonthlyCost)
	}
	if len(result.Recommendations) != 0 {
		t.Fatalf("expected no recommendations for terminated instance, got %d", len(result.Recommendations))
	}
}

func TestAnalyzeHighCostInstance(t *testing.T) {
	resources := []cloud.Resource{
		{
			ID:       "i-456",
			Name:     "big-box",
			Provider: "aws",
			Type:     cloud.ResourceEC2,
			Status:   "running",
			CostUSD:  200.0,
			Tags:     map[string]string{"cloudripper:managed": "true"},
		},
	}

	result := Analyze(resources)
	if len(result.Recommendations) != 1 {
		t.Fatalf("expected 1 recommendation, got %d", len(result.Recommendations))
	}
	if result.Recommendations[0].Action != ActionRightsize {
		t.Errorf("expected rightsize action, got %s", result.Recommendations[0].Action)
	}
}

func TestAnalyzeNoOpportunities(t *testing.T) {
	resources := []cloud.Resource{
		{
			ID:       "i-789",
			Provider: "aws",
			Type:     cloud.ResourceEC2,
			Status:   "running",
			CostUSD:  10.0,
			Tags:     map[string]string{"cloudripper:managed": "true"},
		},
	}

	result := Analyze(resources)
	if len(result.Recommendations) != 0 {
		t.Fatalf("expected 0 recommendations, got %d", len(result.Recommendations))
	}
}
