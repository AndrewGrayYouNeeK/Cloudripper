package optimizer

import (
	"testing"

	"github.com/AndrewGrayYouNeeK/cloudripper/internal/cloud"
)

func TestAnalyzeStoppedInstance(t *testing.T) {
	resources := []cloud.Resource{
		{
			ID:       "i-123",
			Name:     "old-server",
			Provider: "aws",
			Type:     cloud.ResourceEC2,
			Status:   "stopped",
			CostUSD:  7.0,
		},
	}

	result := Analyze(resources)
	if len(result.Recommendations) != 1 {
		t.Fatalf("expected 1 recommendation, got %d", len(result.Recommendations))
	}
	if result.Recommendations[0].Action != ActionTerminate {
		t.Errorf("expected terminate action, got %s", result.Recommendations[0].Action)
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
