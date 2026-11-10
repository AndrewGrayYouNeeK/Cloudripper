package runtime

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/cloud"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/config"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/cost"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/demo"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/notify"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/optimizer"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/watch"
)

func TestLoadDemoReportIsFictionalAndLocal(t *testing.T) {
	report, err := LoadDemoReport()
	if err != nil {
		t.Fatal(err)
	}
	if !report.Fictional || !report.ReadOnly || report.NetworkCalls != 0 {
		t.Fatalf("report flags = fictional %v readonly %v calls %d", report.Fictional, report.ReadOnly, report.NetworkCalls)
	}
	if report.Banner != demo.Banner || !strings.Contains(report.Banner, "not real savings") {
		t.Fatalf("banner = %q", report.Banner)
	}
	if report.SavingsNote != DemoSavingsNote {
		t.Fatalf("note = %q", report.SavingsNote)
	}
	var sawWorker bool
	for _, row := range report.Resources {
		if row.ID == "sample-worker" || row.Name == "sample-worker" {
			sawWorker = true
		}
		if strings.Contains(row.ID, "protected") {
			for _, rec := range report.Recommendations {
				if rec.ID == row.ID {
					t.Fatalf("protected resource recommended: %s", rec.ID)
				}
			}
		}
	}
	if !sawWorker {
		t.Fatal("demo inventory missing sample-worker")
	}
	_, res, err := demo.Load()
	if err != nil {
		t.Fatal(err)
	}
	want := optimizer.Analyze(res.Resources).PotentialSavings
	if report.PotentialSavings != want && len(report.Recommendations) > 0 {
		// Rounded to cents. Allow a one-cent difference from rounding.
		delta := report.PotentialSavings - want
		if delta < -0.01 || delta > 0.01 {
			t.Fatalf("savings = %v want %v", report.PotentialSavings, want)
		}
	}
	html, err := ExportReport("html", report)
	if err != nil {
		t.Fatal(err)
	}
	text := string(html)
	for _, needle := range []string{"FICTIONAL SAMPLE DATA", "n/a", "not real savings"} {
		if !strings.Contains(text, needle) {
			t.Fatalf("html missing %q", needle)
		}
	}
	for _, bad := range []string{"<script", "http://", "https://", "src="} {
		if strings.Contains(text, bad) {
			t.Fatalf("html contains %s", bad)
		}
	}
}

func TestExportEscapesHTML(t *testing.T) {
	report := Report{
		Title:            "CloudRipper rip",
		ReadOnly:         true,
		EstimatedDisplay: "$10.00",
		ActualDisplay:    "unavailable",
		Resources: []ResourceView{{
			Provider: "aws", Type: "ec2", ID: "i-1", Name: "<script>", CostDisplay: "10.00", CostSource: "estimate",
		}},
		Warnings: []string{},
		Skipped:  []string{},
	}
	html, err := ExportReport("html", report)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(html), "&lt;script&gt;") {
		t.Fatalf("name was not escaped: %s", html)
	}
	csv, err := ExportReport("csv", report)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(csv), "PROVIDER") {
		t.Fatalf("csv = %s", csv)
	}
}

func TestProviderStatusRedactsSecrets(t *testing.T) {
	secret := "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
	token := "super-secret-cloudflare-token"
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIAIOSFODNN7EXAMPLE")
	t.Setenv("AWS_SECRET_ACCESS_KEY", secret)
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("GCP_PROJECT", "")
	t.Setenv("AZURE_SUBSCRIPTION_ID", "")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "acct-1234")
	t.Setenv("CLOUDFLARE_API_TOKEN", token)
	dir := t.TempDir()
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(dir, "missing-credentials"))
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(dir, "missing-config"))

	statuses := ProviderStatuses(context.Background(), config.Load())
	blob, err := json.Marshal(statuses)
	if err != nil {
		t.Fatal(err)
	}
	text := string(blob)
	if strings.Contains(text, secret) || strings.Contains(text, token) || strings.Contains(text, "AKIAIOSFODNN7EXAMPLE") {
		t.Fatalf("status leaked a secret: %s", text)
	}
	byID := map[string]ProviderStatus{}
	for _, status := range statuses {
		byID[status.ID] = status
	}
	if byID["aws"].Status != StatusFound {
		t.Fatalf("aws status = %+v", byID["aws"])
	}
	if !strings.Contains(byID["aws"].Identity, "region ") {
		t.Fatalf("aws identity = %q", byID["aws"].Identity)
	}
	if byID["gcp"].Status != StatusMissing || byID["azure"].Status != StatusMissing {
		t.Fatalf("gcp=%s azure=%s", byID["gcp"].Status, byID["azure"].Status)
	}
	if byID["cloudflare"].Status != StatusFound || !strings.Contains(byID["cloudflare"].Identity, "acct-1234") {
		t.Fatalf("cloudflare = %+v", byID["cloudflare"])
	}
}

func TestCloudflareStatusMissingDoesNotEchoToken(t *testing.T) {
	cfg := config.Config{CloudflareAPIToken: "token-value-should-not-appear"}
	status := cloudflareStatus(cfg)
	blob, _ := json.Marshal(status)
	if strings.Contains(string(blob), "token-value-should-not-appear") {
		t.Fatalf("status = %s", blob)
	}
	if status.Status != StatusMissing {
		t.Fatalf("status = %+v", status)
	}
}

func TestFindBillsSampleIgnoresMail(t *testing.T) {
	t.Setenv("CLOUDRIPPER_IMAP_HOST", "127.0.0.1")
	t.Setenv("CLOUDRIPPER_IMAP_PORT", "1")
	t.Setenv("GCP_PROJECT", "")
	t.Setenv("AZURE_SUBSCRIPTION_ID", "")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "")
	t.Setenv("CLOUDFLARE_API_TOKEN", "")
	result, err := FindBills(context.Background(), config.Load(), BillRequest{Sample: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Sample {
		t.Fatal("expected sample result")
	}
	report := billReportFrom(result)
	blob, _ := json.Marshal(report)
	if !strings.Contains(string(blob), "FICTIONAL SAMPLE DATA") || strings.Contains(string(blob), "127.0.0.1") {
		t.Fatalf("report = %s", blob)
	}
	_, err = FindBills(context.Background(), config.Config{}, BillRequest{Sample: true, CSVPath: "card.csv"})
	if err == nil || !strings.Contains(err.Error(), "--sample") {
		t.Fatalf("conflict error = %v", err)
	}
}

func TestSkippedProvidersMatchCLIText(t *testing.T) {
	lines := SkippedProviders(config.Config{})
	text := strings.Join(lines, "\n")
	for _, needle := range []string{
		"skipped: GCP project is not set",
		"skipped: Azure subscription is not set",
		"skipped: Cloudflare is not set",
	} {
		if !strings.Contains(text, needle) {
			t.Fatalf("missing %q in %s", needle, text)
		}
	}
}

func TestBuildProvidersIncludesCloudflare(t *testing.T) {
	providers := BuildProviders(config.Config{CloudflareAccountID: "acct", CloudflareAPIToken: "token"})
	var saw bool
	for _, provider := range providers {
		if provider.Name() == "cloudflare" {
			saw = true
		}
	}
	if !saw {
		t.Fatal("missing cloudflare provider")
	}
}

func TestComingLaterHasNoMutationsOrDiscord(t *testing.T) {
	items := ComingLater()
	blob, _ := json.Marshal(items)
	text := strings.ToLower(string(blob))
	if strings.Contains(text, "discord") {
		t.Fatalf("discord mentioned: %s", blob)
	}
	for _, name := range []string{"Apply", "Scale", "Schedule", "Chaos"} {
		found := false
		for _, item := range items {
			if item.Name == name && strings.Contains(item.Detail, "Coming later") {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing %s in %s", name, blob)
		}
	}
}

type memoryNotifier struct {
	mu   sync.Mutex
	msgs []notify.Message
}

func (m *memoryNotifier) Name() string { return "memory" }

func (m *memoryNotifier) Notify(context.Context, notify.Message) error {
	m.mu.Lock()
	m.msgs = append(m.msgs, notify.Message{Subject: "sent"})
	m.mu.Unlock()
	return nil
}

func TestWatchControllerAlertsWithoutMutating(t *testing.T) {
	webhook := "https://hooks.slack.com/services/T000/B000/secret-token"
	t.Setenv("SLACK_WEBHOOK_URL", webhook)
	var n atomic.Int32
	collector := collectorFunc(func(context.Context) (watch.Snapshot, error) {
		i := n.Add(1)
		total := 10.0
		if i >= 2 {
			total = 40
		}
		return watch.Snapshot{Total: total, ByProvider: map[string]float64{"aws": total}}, nil
	})
	notes := &memoryNotifier{}
	ctl := NewWatchController()
	opts := WatchOptions{
		Interval:  40 * time.Millisecond,
		Threshold: 25,
		StatePath: filepath.Join(t.TempDir(), "watch.json"),
		Notifiers: []notify.Notifier{notes},
		Collector: collector,
		Config:    config.Load(),
		ConfigSet: true,
	}
	if _, err := ctl.Start(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	var state WatchState
	for time.Now().Before(deadline) {
		state = ctl.State()
		if len(state.Alerts) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	ctl.Stop()
	if len(state.Alerts) == 0 {
		t.Fatalf("expected an alert, state=%+v", ctl.State())
	}
	notes.mu.Lock()
	sent := len(notes.msgs)
	notes.mu.Unlock()
	if sent == 0 {
		t.Fatal("notifier was not called")
	}
	blob, _ := json.Marshal(ctl.State())
	if strings.Contains(string(blob), webhook) || strings.Contains(string(blob), "secret-token") {
		t.Fatalf("watch state leaked webhook: %s", blob)
	}
	if !ctl.State().SlackConfigured {
		t.Fatal("expected slack to be reported as configured without revealing the webhook")
	}
}

func TestWatchChannelsHideSecrets(t *testing.T) {
	t.Setenv("SLACK_WEBHOOK_URL", "https://hooks.slack.com/services/SECRET")
	t.Setenv("SMTP_HOST", "smtp.example.com")
	t.Setenv("ALERT_EMAIL_FROM", "from@example.com")
	t.Setenv("ALERT_EMAIL_TO", "to@example.com")
	t.Setenv("SMTP_PASSWORD", "smtp-secret")
	slack, email := WatchChannels(config.Load())
	if !slack || !email {
		t.Fatalf("slack %v email %v", slack, email)
	}
}

func TestRedactSecretsRemovesWebhook(t *testing.T) {
	t.Setenv("SLACK_WEBHOOK_URL", "https://hooks.slack.com/services/SECRETVALUE")
	got := redactSecrets("posted to https://hooks.slack.com/services/SECRETVALUE and AKIAIOSFODNN7EXAMPLE")
	if strings.Contains(got, "SECRETVALUE") || strings.Contains(got, "AKIA") {
		t.Fatalf("redact = %q", got)
	}
}

func TestSnapshotFromPrefersActuals(t *testing.T) {
	snap := SnapshotFrom(cost.Resolution{
		ActualAvailable: true,
		ActualTotal:     6.12,
		Snapshots: []cost.Snapshot{{
			Source:   cloud.CostSourceAWS,
			TotalUSD: 6.12,
		}},
		Resources: []cloud.Resource{{
			Provider: "aws", CostUSD: 99, CostSource: cloud.CostSourceEstimate,
		}},
	})
	if snap.Total != 6.12 || snap.ByProvider["aws"] != 6.12 {
		t.Fatalf("snapshot = %+v", snap)
	}
}
