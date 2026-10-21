package chaos

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestValidateBlastRadius(t *testing.T) {
	policy := DefaultSafety()
	base := Experiment{Name: "exp", Kind: "PodChaos", Target: "payments", Duration: "30s", Parameters: map[string]any{"action": "pod-kill"}}
	if err := Validate(base, policy); err != nil {
		t.Fatal(err)
	}
	cases := []Experiment{
		{Target: "*", Duration: "30s"},
		{Target: "payments", Duration: "10m"},
		{Target: "payments", Duration: "30s", Parameters: map[string]any{"namespace": "kube-system"}},
		{Target: "prod-api", Duration: "30s"},
		{Target: "payments", Duration: "30s", Parameters: map[string]any{"count": 50}},
	}
	for _, exp := range cases {
		if err := Validate(exp, policy); err == nil {
			t.Fatalf("expected rejection for %+v", exp)
		}
	}
	policy.AllowProduction = true
	if err := Validate(Experiment{Target: "prod-api", Duration: "30s"}, policy); err != nil {
		t.Fatal(err)
	}
}

func TestInjectStatusAbortRollback(t *testing.T) {
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method+" "+r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("auth = %q", r.Header.Get("Authorization"))
		}
		body, _ := io.ReadAll(r.Body)
		switch r.Method {
		case http.MethodPost:
			if !strings.Contains(string(body), "pod-kill") {
				t.Errorf("body = %s", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"metadata": map[string]any{"name": "exp-1"}})
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"phase": "finished"})
		case http.MethodDelete:
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			http.Error(w, "nope", http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	orch := NewOrchestrator(Config{Provider: ProviderChaosMesh, Endpoint: server.URL, APIKey: "secret"})
	orch.SetHTTPClient(server.Client())
	exp := NewExperiment("exp-1", "PodChaos", "payments", "30s", "")
	resp, err := orch.Inject(context.Background(), exp)
	if err != nil {
		t.Fatal(err)
	}
	ref := ExperimentRef(ProviderChaosMesh, exp, resp)
	if ref != "exp-1" {
		t.Fatalf("ref = %s", ref)
	}
	status, err := orch.Poll(context.Background(), ref, time.Millisecond, time.Second)
	if err != nil || status.Phase != "finished" {
		t.Fatalf("status %+v err %v", status, err)
	}
	if err := orch.Rollback(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(methods, ",")
	if !strings.Contains(joined, "POST /api/v1/experiments") || !strings.Contains(joined, "DELETE /api/v1/experiments/exp-1") {
		t.Fatalf("methods = %s", joined)
	}
}
