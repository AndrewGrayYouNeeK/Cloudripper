package chaos

import "testing"

func TestBuildEndpointURLWithScheme(t *testing.T) {
	got := buildEndpointURL("https://chaos.example.com", "/api/v1/experiments", "http")
	want := "https://chaos.example.com/api/v1/experiments"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestBuildEndpointURLWithoutScheme(t *testing.T) {
	got := buildEndpointURL("chaos.example.com", "/api/v1/experiments", "http")
	want := "http://chaos.example.com/api/v1/experiments"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestDefaultActionForKind(t *testing.T) {
	if got := DefaultActionForKind("PodChaos"); got != "pod-kill" {
		t.Fatalf("expected pod-kill, got %s", got)
	}
	if got := DefaultActionForKind("NetworkChaos"); got != "delay" {
		t.Fatalf("expected delay, got %s", got)
	}
	if got := DefaultActionForKind("UnknownChaos"); got != "" {
		t.Fatalf("expected empty action for unknown kind, got %s", got)
	}
}

func TestNewExperimentUsesActionNotKind(t *testing.T) {
	exp := NewExperiment("cloudripper-PodChaos", "PodChaos", "cloudripper", "30s", "")

	if exp.Parameters["action"] != "pod-kill" {
		t.Fatalf("expected action pod-kill, got %v", exp.Parameters["action"])
	}
	if exp.Parameters["action"] == exp.Kind {
		t.Fatal("action must not equal kind name")
	}
}

func TestNewExperimentExplicitActionOverride(t *testing.T) {
	exp := NewExperiment("cloudripper-PodChaos", "PodChaos", "cloudripper", "30s", "container-kill")

	if exp.Parameters["action"] != "container-kill" {
		t.Fatalf("expected explicit action container-kill, got %v", exp.Parameters["action"])
	}
}

func TestResolveAction(t *testing.T) {
	if got := ResolveAction("", "PodChaos"); got != "pod-kill" {
		t.Fatalf("expected pod-kill, got %s", got)
	}
	if got := ResolveAction("custom", "PodChaos"); got != "custom" {
		t.Fatalf("expected custom override, got %s", got)
	}
}
