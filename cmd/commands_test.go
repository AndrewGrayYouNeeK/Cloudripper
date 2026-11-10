package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/cloud"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/config"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/cost"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/scale"
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

func TestCostWarningPrintsOnce(t *testing.T) {
	prev := rt
	t.Cleanup(func() { rt = prev })
	warning := "Cost Explorer is not enabled for this account. Enable it in the AWS Billing console; cost data usually appears after about 24 hours."
	rt = runtime{
		scan: func(context.Context, config.Config) ([]cloud.Resource, error) {
			return []cloud.Resource{{ID: "i-1", Provider: "aws", Type: cloud.ResourceEC2, Status: "running", CostUSD: 1}}, nil
		},
		costs: func(context.Context, config.Config, []cloud.Resource) (cost.Resolution, error) {
			return cost.Resolution{
				Resources:      []cloud.Resource{{ID: "i-1", Provider: "aws", Type: cloud.ResourceEC2, CostUSD: 1}},
				EstimatedTotal: 1,
				Warnings:       []string{warning},
			}, nil
		},
		executors: func(context.Context, config.Config) ([]cloud.Executor, error) { return nil, nil },
		scalers:   func(context.Context, config.Config) ([]scale.Controller, error) { return nil, nil },
	}
	for _, command := range []string{"rip", "cost"} {
		out, stderr, err := runCLI(t, command)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(out+stderr, warning) != 1 || strings.Count(out, warning) != 0 {
			t.Fatalf("%s printed the warning %d times\nstdout:\n%s\nstderr:\n%s", command, strings.Count(out+stderr, warning), out, stderr)
		}
	}
}

func TestUnconfiguredCloudsAreSkipped(t *testing.T) {
	t.Setenv("GCP_PROJECT", "")
	t.Setenv("AZURE_SUBSCRIPTION_ID", "")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "")
	t.Setenv("CLOUDFLARE_API_TOKEN", "")
	useRuntime(t, []cloud.Resource{{
		ID: "i-1", Provider: "aws", Type: cloud.ResourceEC2, Status: "running", CostUSD: 1,
	}}, nil, nil, nil)
	_, stderr, err := runCLI(t, "rip")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr, "skipped: GCP project is not set") ||
		!strings.Contains(stderr, "skipped: Azure subscription is not set") ||
		!strings.Contains(stderr, "skipped: Cloudflare is not set") {
		t.Fatalf("stderr = %s", stderr)
	}
}

func TestBuildProvidersIncludesCloudflare(t *testing.T) {
	cfg := config.Config{
		CloudflareAccountID: "acct",
		CloudflareAPIToken:  "token",
	}
	providers := buildProviders(cfg)
	var saw bool
	for _, p := range providers {
		if p.Name() == "cloudflare" {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("providers missing cloudflare: %#v", providers)
	}
}

func TestCloudflareCostUnavailableWarning(t *testing.T) {
	prev := rt
	t.Cleanup(func() { rt = prev })
	warning := "billing-history: cloudflare billing history unavailable (needs Account Billing Read on the API token): permission denied"
	rt = runtime{
		scan: func(context.Context, config.Config) ([]cloud.Resource, error) {
			return []cloud.Resource{{
				ID: "api", Provider: "cloudflare", Type: cloud.ResourceCFWorker, Status: "active", CostUSD: 0, CostSource: cloud.CostSourceUnpriced,
			}}, nil
		},
		costs: func(context.Context, config.Config, []cloud.Resource) (cost.Resolution, error) {
			return cost.Resolution{
				Resources: []cloud.Resource{{
					ID: "api", Provider: "cloudflare", Type: cloud.ResourceCFWorker, CostUSD: 0, CostSource: cloud.CostSourceUnpriced,
				}},
				EstimatedTotal:  0,
				UnpricedCount:   1,
				ActualAvailable: false,
				Warnings:        []string{warning, "estimated_total excludes 1 unpriced resource(s); per-resource cost is unavailable for those rows (not a $0 estimate)"},
			}, nil
		},
		executors: func(context.Context, config.Config) ([]cloud.Executor, error) { return nil, nil },
		scalers:   func(context.Context, config.Config) ([]scale.Controller, error) { return nil, nil },
	}
	out, stderr, err := runCLI(t, "cost")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr, warning) {
		t.Fatalf("stderr = %s", stderr)
	}
	if strings.Contains(out, "Actual spend (trailing window): $0.00") {
		t.Fatalf("must not present missing Cloudflare billing as $0 actual:\n%s", out)
	}
	if !strings.Contains(out, "Actual spend: unavailable") {
		t.Fatalf("output = %s", out)
	}
	if !strings.Contains(out, "n/a") || !strings.Contains(out, "unpriced") {
		t.Fatalf("expected unpriced/n/a presentation, got:\n%s", out)
	}
}

func TestCloudflareUnpricedJSONNullCost(t *testing.T) {
	resources := []cloud.Resource{
		{ID: "api", Provider: "cloudflare", Type: cloud.ResourceCFWorker, Status: "active", CostUSD: 0, CostSource: cloud.CostSourceUnpriced},
		{ID: "i-1", Provider: "aws", Type: cloud.ResourceEC2, Status: "running", CostUSD: 10, CostSource: cloud.CostSourceEstimate},
	}
	prev := rt
	t.Cleanup(func() { rt = prev })
	rt = runtime{
		scan: func(context.Context, config.Config) ([]cloud.Resource, error) {
			return resources, nil
		},
		costs: func(_ context.Context, _ config.Config, got []cloud.Resource) (cost.Resolution, error) {
			return cost.Resolve(context.Background(), got, nil), nil
		},
		executors: func(context.Context, config.Config) ([]cloud.Executor, error) { return nil, nil },
		scalers:   func(context.Context, config.Config) ([]scale.Controller, error) { return nil, nil },
	}
	out, _, err := runCLI(t, "--output", "json", "rip")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"cost_usd": null`) || !strings.Contains(out, `"cost_source": "unpriced"`) {
		t.Fatalf("expected null unpriced cost, got %s", out)
	}
	if !strings.Contains(out, `"estimated_total": 10`) || !strings.Contains(out, `"unpriced_count": 1`) {
		t.Fatalf("mixed totals incomplete coverage missing: %s", out)
	}
	if !strings.Contains(out, `"cost_usd": 10`) {
		t.Fatalf("aws priced row should keep numeric cost: %s", out)
	}
}

func TestCostJSONMatchesRipSchemaAndRoundsTotals(t *testing.T) {
	resources := []cloud.Resource{
		{ID: "api", Provider: "cloudflare", Type: cloud.ResourceCFWorker, Status: "active", CostUSD: 0, CostSource: cloud.CostSourceUnpriced},
		{ID: "i-1", Provider: "aws", Type: cloud.ResourceEC2, Status: "running", CostUSD: 10, CostSource: cloud.CostSourceEstimate},
		{ID: "fn", Provider: "aws", Type: cloud.ResourceLambda, Status: "active", CostUSD: 0, CostSource: cloud.CostSourceEstimate},
	}
	// 6.12 (Cloudflare billing) plus a tiny Cost Explorer residual encodes as 6.1199999999.
	ugly := 6.12 + (-1e-10)
	resolution := cost.Resolution{
		Resources:       resources,
		EstimatedTotal:  10,
		UnpricedCount:   1,
		ActualTotal:     ugly,
		ActualAvailable: true,
		Snapshots: []cost.Snapshot{
			{
				Source:   cloud.CostSourceCloudflareBilling,
				TotalUSD: 6.12,
				Services: []cost.ServiceCost{{Service: "Workers Paid", USD: 6.12}},
			},
			{
				Source:   cloud.CostSourceAWS,
				TotalUSD: -1e-10,
				Services: []cost.ServiceCost{{Service: "AmazonEC2", USD: -1e-10}},
			},
		},
	}
	prev := rt
	t.Cleanup(func() { rt = prev })
	rt = runtime{
		scan: func(context.Context, config.Config) ([]cloud.Resource, error) {
			return resources, nil
		},
		costs: func(context.Context, config.Config, []cloud.Resource) (cost.Resolution, error) {
			return resolution, nil
		},
		executors: func(context.Context, config.Config) ([]cloud.Executor, error) { return nil, nil },
		scalers:   func(context.Context, config.Config) ([]scale.Controller, error) { return nil, nil },
	}

	for _, command := range []string{"cost", "rip"} {
		out, _, err := runCLI(t, "--output", "json", command)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, `"CostUSD"`) || strings.Contains(out, `"ID"`) || strings.Contains(out, "6.1199999999") {
			t.Fatalf("%s json kept Go field names or float residue:\n%s", command, out)
		}
		var doc struct {
			DryRun         bool    `json:"dry_run"`
			EstimatedTotal float64 `json:"estimated_total"`
			ActualTotal    float64 `json:"actual_total"`
			Resources      []struct {
				ID         string   `json:"id"`
				Provider   string   `json:"provider"`
				CostUSD    *float64 `json:"cost_usd"`
				CostSource string   `json:"cost_source"`
			} `json:"resources"`
			Snapshots []struct {
				Source   string  `json:"source"`
				TotalUSD float64 `json:"total_usd"`
				Services []struct {
					USD float64 `json:"usd"`
				} `json:"services"`
			} `json:"snapshots"`
		}
		if err := json.Unmarshal([]byte(out), &doc); err != nil {
			t.Fatalf("%s json: %v\n%s", command, err, out)
		}
		if doc.EstimatedTotal != 10 || doc.ActualTotal != 6.12 {
			t.Fatalf("%s totals estimated=%v actual=%v", command, doc.EstimatedTotal, doc.ActualTotal)
		}
		byID := map[string]struct {
			CostUSD    *float64
			CostSource string
		}{}
		for _, row := range doc.Resources {
			byID[row.ID] = struct {
				CostUSD    *float64
				CostSource string
			}{row.CostUSD, row.CostSource}
		}
		api := byID["api"]
		if api.CostUSD != nil || api.CostSource != cloud.CostSourceUnpriced {
			t.Fatalf("%s unpriced row = %+v", command, api)
		}
		aws := byID["i-1"]
		if aws.CostUSD == nil || *aws.CostUSD != 10 || aws.CostSource != cloud.CostSourceEstimate {
			t.Fatalf("%s priced aws row = %+v", command, aws)
		}
		lambda := byID["fn"]
		if lambda.CostUSD == nil || *lambda.CostUSD != 0 {
			t.Fatalf("%s zero estimate must stay numeric, got %+v", command, lambda)
		}
		if len(doc.Snapshots) != 2 || doc.Snapshots[0].TotalUSD != 6.12 || doc.Snapshots[1].TotalUSD != 0 {
			t.Fatalf("%s snapshots = %+v", command, doc.Snapshots)
		}
		if doc.Snapshots[1].Services[0].USD != 0 {
			t.Fatalf("%s residual service usd = %v", command, doc.Snapshots[1].Services[0].USD)
		}
	}
}

func TestCloudflareProviderSelectionAndNoMutation(t *testing.T) {
	resetCmdFlags(t)
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "acct")
	t.Setenv("CLOUDFLARE_API_TOKEN", "token")
	cfg := loadConfig()
	providers := buildProviders(cfg)
	var cf cloud.Executor
	for _, p := range providers {
		if p.Name() == "cloudflare" {
			if exec, ok := p.(cloud.Executor); ok {
				cf = exec
			}
		}
	}
	if cf == nil {
		t.Fatal("expected cloudflare executor from buildProviders")
	}
	_, err := cf.Execute(context.Background(), cloud.Resource{
		ID: "api", Provider: "cloudflare", Type: cloud.ResourceCFWorker,
	}, "terminate", "")
	if err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("expected read-only refuse, got %v", err)
	}
}

func TestFindBillsCSV(t *testing.T) {
	t.Setenv("CLOUDRIPPER_IMAP_HOST", "")
	t.Setenv("CLOUDRIPPER_GMAIL_TOKEN", "")
	t.Setenv("GCP_PROJECT", "")
	t.Setenv("AZURE_SUBSCRIPTION_ID", "")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "")
	t.Setenv("CLOUDFLARE_API_TOKEN", "")
	path := t.TempDir() + "/card.csv"
	body := "Date,Description,Amount\n2026-09-02,DIGITALOCEAN.COM,-15.00\n2026-09-03,AMAZON WEB SERVICES,-42.50\n2026-09-04,CLOUDFLARE,-9.00\n2026-08-01,STARBUCKS,-4.50\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	out, stderr, err := runCLI(t, "--output", "json", "find-bills", "--csv", path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out+stderr, "s3cret") {
		t.Fatal("output contained a secret")
	}
	if !strings.Contains(out, "DigitalOcean") || !strings.Contains(out, "possibly forgotten") {
		t.Fatalf("output = %s", out)
	}
	if !strings.Contains(out, "AWS") || !strings.Contains(out, "configured") {
		t.Fatalf("output = %s", out)
	}
	if !strings.Contains(out, "Cloudflare") || !strings.Contains(out, "possibly forgotten") {
		t.Fatalf("expected unconfigured Cloudflare bill, got %s", out)
	}
	if strings.Contains(strings.ToLower(out), "starbucks") {
		t.Fatalf("output = %s", out)
	}

	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "acct")
	t.Setenv("CLOUDFLARE_API_TOKEN", "token-not-for-live-use")
	out2, _, err := runCLI(t, "--output", "json", "find-bills", "--csv", path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out2, "Cloudflare") || !strings.Contains(out2, `"configured": "true"`) {
		t.Fatalf("expected Cloudflare configured when account+token set, got %s", out2)
	}
	if strings.Contains(out2, "token-not-for-live-use") {
		t.Fatal("token leaked into find-bills output")
	}
}

func TestUnknownOutputFormat(t *testing.T) {
	useRuntime(t, []cloud.Resource{{ID: "i-1", Provider: "aws", Type: cloud.ResourceEC2}}, nil, nil, nil)
	_, _, err := runCLI(t, "--output", "xml", "rip")
	if err == nil || !strings.Contains(err.Error(), "xml") {
		t.Fatalf("expected format error, got %v", err)
	}
}

func TestMissingCredentialsGivesFriendlyError(t *testing.T) {
	awsErr := errors.New("aws: ec2: operation error EC2: DescribeInstances, get identity: get credentials: failed to refresh cached credentials, no EC2 IMDS role found, operation error ec2imds: GetMetadata, request canceled, context deadline exceeded")
	useRuntime(t, nil, awsErr, nil, nil)

	for _, command := range []string{"rip", "cost", "optimize"} {
		out, stderr, err := runCLI(t, command)
		if err == nil {
			t.Fatalf("%s: expected an error without credentials", command)
		}
		if !isCredentialsError(err) {
			t.Fatalf("%s: expected a credentials error, got %v", command, err)
		}
		msg := err.Error()
		for _, want := range []string{"AWS", "aws configure", "AWS_PROFILE", "gcloud auth application-default login", "az login", "CLOUDFLARE_ACCOUNT_ID", "CLOUDFLARE_API_TOKEN", "README", "#readme"} {
			if !strings.Contains(msg, want) {
				t.Fatalf("%s: message missing %q:\n%s", command, want, msg)
			}
		}
		for _, bad := range []string{"IMDS", "context deadline", "ec2imds", "DescribeInstances"} {
			if strings.Contains(msg, bad) {
				t.Fatalf("%s: message leaks SDK detail %q:\n%s", command, bad, msg)
			}
		}
		if strings.Count(msg, "\n") > 8 {
			t.Fatalf("%s: message is too long:\n%s", command, msg)
		}
		if strings.Contains(out+stderr, "Usage:") || strings.Contains(out+stderr, "Global Flags") {
			t.Fatalf("%s: usage text should not print for a runtime error:\n%s%s", command, out, stderr)
		}
	}
}

func TestMissingCredentialsListsEachProvider(t *testing.T) {
	err := friendlyScanError(errors.Join(
		errors.New("aws: get credentials: failed to refresh cached credentials"),
		errors.New("gcp: google: could not find default credentials"),
		errors.New("azure: DefaultAzureCredential: failed to acquire a token"),
	))
	msg := err.Error()
	for _, want := range []string{"AWS, Azure, GCP", "aws configure", "gcloud auth application-default login", "az login"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message missing %q:\n%s", want, msg)
		}
	}
}

func TestOtherScanErrorsStayAsTheyAre(t *testing.T) {
	err := friendlyScanError(errors.New("aws: ec2: AccessDenied: not authorized"))
	if isCredentialsError(err) || !strings.Contains(err.Error(), "scan failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBadFlagStillShowsUsage(t *testing.T) {
	useRuntime(t, nil, nil, nil, nil)
	out, stderr, err := runCLI(t, "rip", "--no-such-flag")
	stderr += out
	if err == nil {
		t.Fatal("expected an error for an unknown flag")
	}
	if !strings.Contains(stderr, "Usage:") {
		t.Fatalf("usage should print for a bad flag, got %q", stderr)
	}
}
