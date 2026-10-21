package cmd

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
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
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetIn(bytes.NewReader(nil))
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		resetFlagSet(t, cmd.Flags())
		resetFlagSet(t, cmd.PersistentFlags())
		for _, child := range cmd.Commands() {
			walk(child)
		}
	}
	walk(rootCmd)
	os.Unsetenv("DRY_RUN")
	os.Unsetenv("CLOUDRIPPER_OUTPUT")
}

func resetFlagSet(t *testing.T, set *pflag.FlagSet) {
	t.Helper()
	set.VisitAll(func(f *pflag.Flag) {
		if err := f.Value.Set(f.DefValue); err != nil {
			t.Fatalf("reset flag %s: %v", f.Name, err)
		}
		f.Changed = false
	})
}
