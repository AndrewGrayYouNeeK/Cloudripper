package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/AndrewGrayYouNeeK/cloudripper/internal/cloud"
	"github.com/AndrewGrayYouNeeK/cloudripper/internal/config"
	"github.com/AndrewGrayYouNeeK/cloudripper/internal/cost"
	"github.com/AndrewGrayYouNeeK/cloudripper/internal/scale"
)

type recordingExec struct {
	calls []string
}

func (r *recordingExec) Provider() string { return "aws" }

func (r *recordingExec) Execute(_ context.Context, res cloud.Resource, action, spec string) (string, error) {
	r.calls = append(r.calls, action+" "+res.ID+" "+spec)
	return "ok", nil
}

type recordingScaler struct {
	applied int
	cpu     float64
	groups  []scale.Group
}

func (r *recordingScaler) Provider() string { return "aws" }

func (r *recordingScaler) Discover(context.Context) ([]scale.Group, error) {
	return r.groups, nil
}

func (r *recordingScaler) Lookup(_ context.Context, kind, name string) (scale.Group, error) {
	for _, group := range r.groups {
		if group.Kind == kind && group.Name == name {
			return group, nil
		}
	}
	return scale.Group{}, context.Canceled
}

func (r *recordingScaler) CPU(context.Context, scale.Group) (float64, error) {
	return r.cpu, nil
}

func (r *recordingScaler) Apply(context.Context, scale.Group, scale.Decision) error {
	r.applied++
	return nil
}

func useRuntime(t *testing.T, resources []cloud.Resource, scanErr error, exec *recordingExec, scaler *recordingScaler) {
	t.Helper()
	prev := rt
	t.Cleanup(func() { rt = prev })
	if exec == nil {
		exec = &recordingExec{}
	}
	if scaler == nil {
		scaler = &recordingScaler{}
	}
	rt = runtime{
		scan: func(context.Context, config.Config) ([]cloud.Resource, error) {
			return resources, scanErr
		},
		costs: func(_ context.Context, _ config.Config, got []cloud.Resource) (cost.Resolution, error) {
			return cost.Resolution{Resources: got, EstimatedTotal: sumCost(got)}, nil
		},
		executors: func(context.Context, config.Config) ([]cloud.Executor, error) {
			return []cloud.Executor{exec}, nil
		},
		scalers: func(context.Context, config.Config) ([]scale.Controller, error) {
			return []scale.Controller{scaler}, nil
		},
	}
}

func runCLI(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	resetCmdFlags(t)
	out := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	rootCmd.SetOut(out)
	rootCmd.SetErr(errBuf)
	rootCmd.SetIn(bytes.NewReader(nil))
	rootCmd.SetArgs(args)
	err := rootCmd.Execute()
	return out.String(), errBuf.String(), err
}

func TestRipJSONAndPartialScan(t *testing.T) {
	useRuntime(t, []cloud.Resource{{
		ID: "i-1", Provider: "aws", Type: cloud.ResourceEC2, Status: "running", CostUSD: 10, CostSource: cloud.CostSourceEstimate,
	}}, context.Canceled, nil, nil)

	out, stderr, err := runCLI(t, "--output", "json", "rip")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"id": "i-1"`) {
		t.Fatalf("json output missing resource: %s", out)
	}
	if !strings.Contains(stderr, "warning:") {
		t.Fatalf("expected partial scan warning, got %q", stderr)
	}
}

func TestRipCSV(t *testing.T) {
	useRuntime(t, []cloud.Resource{{
		ID: "i-2", Name: "=cmd", Provider: "aws", Type: cloud.ResourceEC2, Status: "running", CostUSD: 3,
	}}, nil, nil, nil)
	out, _, err := runCLI(t, "--output", "csv", "rip")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "'=cmd") {
		t.Fatalf("csv should neutralize formula names, got %s", out)
	}
}

func TestOptimizeApplyDryRunDoesNotExecute(t *testing.T) {
	exec := &recordingExec{}
	useRuntime(t, []cloud.Resource{{
		ID: "i-stop", Provider: "aws", Type: cloud.ResourceEC2, Status: "stopped", CostUSD: 8,
	}}, nil, exec, nil)
	out, _, err := runCLI(t, "--dry-run", "--output", "json", "optimize", "--apply")
	if err != nil {
		t.Fatal(err)
	}
	if len(exec.calls) != 0 {
		t.Fatalf("dry-run called executor: %v", exec.calls)
	}
	if !strings.Contains(out, "dry-run") {
		t.Fatalf("expected dry-run outcome, got %s", out)
	}
}

func TestOptimizeApplyRequiresConfirmation(t *testing.T) {
	exec := &recordingExec{}
	useRuntime(t, []cloud.Resource{{
		ID: "i-stop", Provider: "aws", Type: cloud.ResourceEC2, Status: "stopped", CostUSD: 8,
	}}, nil, exec, nil)
	_, _, err := runCLI(t, "optimize", "--apply")
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("expected confirmation error, got %v", err)
	}
	if len(exec.calls) != 0 {
		t.Fatalf("executor ran without confirmation: %v", exec.calls)
	}
}

func TestOptimizeApplyYes(t *testing.T) {
	exec := &recordingExec{}
	useRuntime(t, []cloud.Resource{{
		ID: "i-stop", Provider: "aws", Type: cloud.ResourceEC2, Status: "stopped", CostUSD: 8,
	}}, nil, exec, nil)
	dir := t.TempDir()
	out, _, err := runCLI(t, "--yes", "--audit-log", dir+"/audit.log", "--output", "json", "optimize", "--apply")
	if err != nil {
		t.Fatal(err)
	}
	if len(exec.calls) != 1 || !strings.Contains(exec.calls[0], "terminate i-stop") {
		t.Fatalf("calls = %v", exec.calls)
	}
	if !strings.Contains(out, `"outcome": "applied"`) {
		t.Fatalf("output = %s", out)
	}
}

func TestScalePreviewDoesNotApply(t *testing.T) {
	scaler := &recordingScaler{
		cpu: 90,
		groups: []scale.Group{{
			Provider: "aws", Kind: scale.KindASG, Name: "web", Desired: 2, Min: 1, Max: 4,
		}},
	}
	useRuntime(t, nil, nil, nil, scaler)
	out, _, err := runCLI(t, "--output", "json", "scale")
	if err != nil {
		t.Fatal(err)
	}
	if scaler.applied != 0 {
		t.Fatalf("preview applied %d times", scaler.applied)
	}
	if !strings.Contains(out, "dry-run") || !strings.Contains(out, "scale-out") {
		t.Fatalf("output = %s", out)
	}
}

func TestWatchOnce(t *testing.T) {
	useRuntime(t, []cloud.Resource{{
		ID: "i-1", Provider: "aws", Type: cloud.ResourceEC2, Status: "running", CostUSD: 12,
	}}, nil, nil, nil)
	out, _, err := runCLI(t, "watch", "--once")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "scan total=$12.00") {
		t.Fatalf("output = %s", out)
	}
}

func TestWatchEnforceRequiresYes(t *testing.T) {
	useRuntime(t, nil, nil, nil, nil)
	_, _, err := runCLI(t, "watch", "--once", "--enforce-schedules")
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("expected --yes error, got %v", err)
	}
}

func TestChaosPlanWithoutEndpoint(t *testing.T) {
	t.Setenv("CHAOS_ENDPOINT", "")
	useRuntime(t, nil, nil, nil, nil)
	out, _, err := runCLI(t, "chaos")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "pod-kill") || !strings.Contains(out, "CHAOS_ENDPOINT") {
		t.Fatalf("output = %s", out)
	}
}

func TestChaosDryRunRejectsProduction(t *testing.T) {
	t.Setenv("CHAOS_ENDPOINT", "http://chaos.example")
	useRuntime(t, nil, nil, nil, nil)
	_, _, err := runCLI(t, "--dry-run", "chaos", "--target", "prod-api", "--duration", "30s")
	if err == nil || !strings.Contains(err.Error(), "production") {
		t.Fatalf("expected production rejection, got %v", err)
	}
}

func TestUnknownOutputFormat(t *testing.T) {
	useRuntime(t, []cloud.Resource{{ID: "i-1", Provider: "aws", Type: cloud.ResourceEC2}}, nil, nil, nil)
	_, _, err := runCLI(t, "--output", "xml", "rip")
	if err == nil || !strings.Contains(err.Error(), "xml") {
		t.Fatalf("expected format error, got %v", err)
	}
}
