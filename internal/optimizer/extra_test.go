package optimizer

import (
	"testing"
	"time"

	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/cloud"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/schedule"
)

func TestAnalyzeSkipsProtectedAndIdleDisk(t *testing.T) {
	result := Analyze([]cloud.Resource{
		{
			ID: "i-keep", Provider: "aws", Type: cloud.ResourceEC2, Status: "running", CostUSD: 20,
			Tags: map[string]string{cloud.ProtectTag: "true"},
		},
		{
			ID: "vol-1", Provider: "aws", Type: cloud.ResourceEBS, Status: "available", CostUSD: 4,
		},
		{
			ID: "db-1", Provider: "aws", Type: cloud.ResourceRDS, Status: "stopped", CostUSD: 12,
		},
	})
	if len(result.Recommendations) != 1 || result.Recommendations[0].Resource.ID != "vol-1" {
		t.Fatalf("recs = %+v", result.Recommendations)
	}
	if result.Recommendations[0].Action != ActionTerminate {
		t.Fatalf("action = %s", result.Recommendations[0].Action)
	}
}

func TestAnalyzeUnmanagedSchedule(t *testing.T) {
	result := Analyze([]cloud.Resource{{
		ID: "i-day", Provider: "aws", Type: cloud.ResourceEC2, Status: "running", CostUSD: 20,
	}})
	if len(result.Recommendations) != 1 || result.Recommendations[0].Action != ActionSchedule {
		t.Fatalf("recs = %+v", result.Recommendations)
	}
	if result.Recommendations[0].SuggestedSpec != schedule.DefaultOffWindow {
		t.Fatalf("spec = %s", result.Recommendations[0].SuggestedSpec)
	}
}

func TestPlanSchedules(t *testing.T) {
	night := time.Date(2026, 1, 2, 20, 0, 0, 0, time.UTC)
	day := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	resources := []cloud.Resource{{
		ID: "i-1", Provider: "aws", Type: cloud.ResourceEC2, Status: "running",
		Tags: map[string]string{schedule.Tag: schedule.DefaultOffWindow},
	}, {
		ID: "i-2", Provider: "aws", Type: cloud.ResourceEC2, Status: "stopped",
		Tags: map[string]string{schedule.Tag: schedule.DefaultOffWindow},
	}}
	nightRecs := PlanSchedules(resources, night)
	if len(nightRecs) != 1 || nightRecs[0].Action != ActionStop || nightRecs[0].Resource.ID != "i-1" {
		t.Fatalf("night = %+v", nightRecs)
	}
	dayRecs := PlanSchedules(resources, day)
	if len(dayRecs) != 1 || dayRecs[0].Action != ActionStart || dayRecs[0].Resource.ID != "i-2" {
		t.Fatalf("day = %+v", dayRecs)
	}
}
