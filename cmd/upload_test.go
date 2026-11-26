package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/cloud"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/pro"
)

type proStub struct {
	mu      sync.Mutex
	uploads []map[string]any
	status  int
	auth    []string
}

func newProStub(t *testing.T) (*proStub, *httptest.Server) {
	t.Helper()
	stub := &proStub{status: http.StatusCreated}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var up map[string]any
		_ = json.Unmarshal(body, &up)
		stub.mu.Lock()
		stub.uploads = append(stub.uploads, up)
		stub.auth = append(stub.auth, r.Header.Get("Authorization"))
		status := stub.status
		stub.mu.Unlock()
		w.WriteHeader(status)
		if status != http.StatusCreated {
			_, _ = w.Write([]byte(`{"ok":false,"error":"nope"}`))
			return
		}
		kind, _ := up["kind"].(string)
		_, _ = w.Write([]byte(`{"ok":true,"report":{"id":"r1","kind":"` + kind + `","account":{"id":"a1","label":"prod-aws"},"resource_count":1}}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv(pro.EnvURL, srv.URL)
	t.Setenv(pro.EnvToken, "crp_testtoken")
	return stub, srv
}

func TestUploadScansAndSendsRipReport(t *testing.T) {
	useRuntime(t, []cloud.Resource{{ID: "i-up", Provider: "aws", Type: cloud.ResourceEC2, Status: "running", CostUSD: 4}}, nil, nil, nil)
	stub, _ := newProStub(t)
	out, _, err := runCLI(t, "upload", "--account", "prod-aws")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `Uploaded rip report r1 to account "prod-aws", 1 resources.`) {
		t.Fatalf("unexpected output %q", out)
	}
	if len(stub.uploads) != 1 || stub.auth[0] != "Bearer crp_testtoken" {
		t.Fatalf("expected one authenticated upload, got %d %v", len(stub.uploads), stub.auth)
	}
	up := stub.uploads[0]
	if up["account"] != "prod-aws" || up["kind"] != "rip" {
		t.Fatalf("bad envelope: %v", up)
	}
	if _, err := time.Parse(time.RFC3339, up["generated_at"].(string)); err != nil {
		t.Fatalf("generated_at: %v", err)
	}
	report := up["report"].(map[string]any)
	resources := report["resources"].([]any)
	if resources[0].(map[string]any)["id"] != "i-up" || report["estimated_total"].(float64) != 4 {
		t.Fatalf("report should be the rip --output json document: %v", report)
	}
}

func TestUploadFileWithKind(t *testing.T) {
	stub, _ := newProStub(t)
	path := filepath.Join(t.TempDir(), "cost.json")
	if err := os.WriteFile(path, []byte(`{"estimated_total":1,"resources":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runCLI(t, "upload", "--account", "prod-aws", "--file", path, "--kind", "cost"); err != nil {
		t.Fatal(err)
	}
	if stub.uploads[0]["kind"] != "cost" {
		t.Fatalf("want kind cost, got %v", stub.uploads[0]["kind"])
	}
	if _, _, err := runCLI(t, "upload", "--account", "prod-aws", "--file", path, "--kind", "optimize"); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("want kind mismatch error, got %v", err)
	}
}

func TestUploadFileFromStdinInfersOptimize(t *testing.T) {
	stub, _ := newProStub(t)
	resetCmdFlags(t)
	out := &bytes.Buffer{}
	rootCmd.SetOut(out)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetIn(strings.NewReader(`{"TotalMonthlyCost":3,"PotentialSavings":1,"Recommendations":[]}`))
	rootCmd.SetArgs([]string{"upload", "--account", "prod-aws", "--file", "-"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if stub.uploads[0]["kind"] != "optimize" {
		t.Fatalf("want inferred optimize, got %v", stub.uploads[0]["kind"])
	}
}

func TestUploadFlagValidation(t *testing.T) {
	newProStub(t)
	cases := [][]string{
		{"upload"},
		{"upload", "--account", "x", "--kind", "cost"},
		{"upload", "--account", "x", "--file", "a.json", "--kind", "chaos"},
		{"upload", "--account", "x", "--every", "1m"},
		{"upload", "--account", "x", "--every", "1h", "--file", "a.json"},
	}
	for _, args := range cases {
		if _, _, err := runCLI(t, args...); err == nil {
			t.Fatalf("%v: expected error", args)
		}
	}
	t.Setenv(pro.EnvToken, "")
	if _, _, err := runCLI(t, "upload", "--account", "x"); err == nil || !strings.Contains(err.Error(), pro.EnvToken) {
		t.Fatalf("want missing token error, got %v", err)
	}
}

func TestUploadReportsServerRejection(t *testing.T) {
	useRuntime(t, []cloud.Resource{{ID: "i-1", Provider: "aws", Type: cloud.ResourceEC2}}, nil, nil, nil)
	stub, _ := newProStub(t)
	stub.mu.Lock()
	stub.status = http.StatusPaymentRequired
	stub.mu.Unlock()
	_, _, err := runCLI(t, "upload", "--account", "prod-aws")
	if err == nil || !strings.Contains(err.Error(), "HTTP 402") {
		t.Fatalf("want 402 error, got %v", err)
	}
}

func TestRunEveryRetriesAndStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	stderr := &bytes.Buffer{}
	err := runEvery(ctx, stderr, time.Millisecond, func(context.Context) error {
		calls++
		switch calls {
		case 1:
			return errors.New("network down")
		case 3:
			cancel()
		}
		return nil
	})
	if err != nil || calls != 3 {
		t.Fatalf("runEvery = %v after %d calls", err, calls)
	}
	if !strings.Contains(stderr.String(), "network down") {
		t.Fatalf("transient error should be reported, got %q", stderr.String())
	}

	calls = 0
	err = runEvery(context.Background(), io.Discard, time.Millisecond, func(context.Context) error {
		calls++
		return &pro.Error{StatusCode: http.StatusUnauthorized, Message: "bad token"}
	})
	if err == nil || calls != 1 {
		t.Fatalf("permanent error should stop the loop: %v after %d calls", err, calls)
	}
}

func TestUploadEveryLoopsUntilCancelled(t *testing.T) {
	useRuntime(t, []cloud.Resource{{ID: "i-1", Provider: "aws", Type: cloud.ResourceEC2}}, nil, nil, nil)
	stub, _ := newProStub(t)
	prev := minUploadInterval
	minUploadInterval = time.Millisecond
	t.Cleanup(func() { minUploadInterval = prev })
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	resetCmdFlags(t)
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetArgs([]string{"upload", "--account", "prod-aws", "--every", "20ms"})
	// Cobra keeps a subcommand's context from an earlier Execute, so set it directly.
	uploadCmd.SetContext(ctx)
	t.Cleanup(func() { uploadCmd.SetContext(context.Background()) })
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if len(stub.uploads) < 2 {
		t.Fatalf("expected repeated uploads, got %d", len(stub.uploads))
	}
}
