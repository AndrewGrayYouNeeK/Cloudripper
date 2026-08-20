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
}
