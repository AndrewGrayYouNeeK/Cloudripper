package config

import (
	"testing"
	"time"
)

func TestLoadEnv(t *testing.T) {
	t.Setenv("AWS_REGION", "eu-west-1")
	t.Setenv("GCP_BILLING_TABLE", "p.d.t")
	t.Setenv("AZURE_SUBSCRIPTION_ID", "sub")
	t.Setenv("WATCH_INTERVAL", "5m")
	t.Setenv("WATCH_THRESHOLD", "40")
	t.Setenv("CHAOS_MAX_TARGETS", "3")
	t.Setenv("DRY_RUN", "true")

	cfg := Load()
	if cfg.AWSRegion != "eu-west-1" || cfg.GCPBillingTable != "p.d.t" || cfg.AzureSubscription != "sub" {
		t.Fatalf("cfg = %+v", cfg)
	}
	if cfg.WatchInterval != 5*time.Minute || cfg.WatchThreshold != 40 || cfg.ChaosMaxTargets != 3 || !cfg.DryRun {
		t.Fatalf("cfg = %+v", cfg)
	}
	if cfg.AuditLog != "cloudripper-audit.log" {
		t.Fatalf("audit = %s", cfg.AuditLog)
	}
}
