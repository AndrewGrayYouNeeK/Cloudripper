package cmd

import (
	"os"
	"testing"
)

func TestLoadConfigPreservesDryRunEnv(t *testing.T) {
	resetCmdFlags(t)
	t.Setenv("DRY_RUN", "true")

	mustParseFlags(t, []string{})
	cfg := loadConfig()
	if !cfg.DryRun {
		t.Fatal("expected DRY_RUN=true from env to be preserved when --dry-run is omitted")
	}
}

func TestLoadConfigDryRunFlagOverridesEnv(t *testing.T) {
	resetCmdFlags(t)
	t.Setenv("DRY_RUN", "true")

	mustParseFlags(t, []string{"--dry-run=false"})
	cfg := loadConfig()
	if cfg.DryRun {
		t.Fatal("expected explicit --dry-run=false to disable dry-run")
	}
}

func TestLoadConfigDryRunFlagEnablesWithoutEnv(t *testing.T) {
	resetCmdFlags(t)
	os.Unsetenv("DRY_RUN")

	mustParseFlags(t, []string{"--dry-run"})
	cfg := loadConfig()
	if !cfg.DryRun {
		t.Fatal("expected --dry-run flag to enable dry-run")
	}
}

func mustParseFlags(t *testing.T, args []string) {
	t.Helper()
	if err := rootCmd.ParseFlags(args); err != nil {
		t.Fatalf("parse flags: %v", err)
	}
}

func resetCmdFlags(t *testing.T) {
	t.Helper()
	rootCmd.SetArgs(nil)
	dryRun = false
	awsRegion = ""
	gcpProject = ""
	for _, name := range []string{"dry-run", "aws-region", "gcp-project"} {
		if f := rootCmd.PersistentFlags().Lookup(name); f != nil {
			_ = f.Value.Set(f.DefValue)
			f.Changed = false
		}
	}
	os.Unsetenv("DRY_RUN")
}
