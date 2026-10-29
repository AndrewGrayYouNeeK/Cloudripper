package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/audit"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/chaos"
	"github.com/spf13/cobra"
)

var (
	chaosTarget      string
	chaosDuration    string
	chaosKind        string
	chaosAction      string
	chaosCount       int
	chaosNamespace   string
	chaosWait        bool
	chaosRollback    bool
	chaosAllowProd   bool
	chaosMaxDuration string
	chaosRef         string
)

var chaosCmd = &cobra.Command{
	Use:   "chaos",
	Short: "Inject a bounded chaos experiment via Chaos Mesh or Gremlin",
	Long: `Posts one experiment to CHAOS_ENDPOINT. The blast-radius check rejects
empty or wildcard targets, durations above the limit, the kube-system,
kube-public, and kube-node-lease namespaces, and names that look like
production unless --allow-prod is set.

--dry-run validates and prints the experiment without posting it. A live
injection asks for confirmation unless --yes is set. --wait polls status.
A failed experiment is aborted when --rollback is left at its default.
Rollback deletes the Chaos Mesh experiment or halts the Gremlin attack. It
does not undo cloud mutations.`,
	RunE: runChaos,
}

var chaosAbortCmd = &cobra.Command{
	Use:   "abort",
	Short: "Abort a running experiment",
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runChaosControl(cmd, true)
	},
}

var chaosStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Fetch experiment status",
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runChaosControl(cmd, false)
	},
}

var chaosRollbackCmd = &cobra.Command{
	Use:   "rollback",
	Short: "Abort an experiment (the supported chaos undo)",
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runChaosControl(cmd, true)
	},
}

func init() {
	chaosCmd.Flags().StringVar(&chaosTarget, "target", "cloudripper", "target application label or namespace/name")
	chaosCmd.Flags().StringVar(&chaosDuration, "duration", "30s", "experiment duration")
	chaosCmd.Flags().StringVar(&chaosKind, "kind", "PodChaos", "chaos experiment kind")
	chaosCmd.Flags().StringVar(&chaosAction, "action", "", "chaos action (defaults from the kind, for example pod-kill)")
	chaosCmd.Flags().IntVar(&chaosCount, "count", 1, "target count")
	chaosCmd.Flags().StringVar(&chaosNamespace, "namespace", "", "Kubernetes namespace")
	chaosCmd.Flags().BoolVar(&chaosWait, "wait", false, "poll until the experiment finishes or fails")
	chaosCmd.Flags().BoolVar(&chaosRollback, "rollback", true, "abort the experiment if it fails while waiting")
	chaosCmd.Flags().BoolVar(&chaosAllowProd, "allow-prod", false, "allow targets whose name looks like production")
	chaosCmd.Flags().StringVar(&chaosMaxDuration, "max-duration", "", "override CHAOS_MAX_DURATION for this command")
	for _, sub := range []*cobra.Command{chaosAbortCmd, chaosStatusCmd, chaosRollbackCmd} {
		sub.Flags().StringVar(&chaosRef, "name", "", "experiment name or provider id")
		_ = sub.MarkFlagRequired("name")
	}
	chaosCmd.AddCommand(chaosAbortCmd, chaosStatusCmd, chaosRollbackCmd)
	rootCmd.AddCommand(chaosCmd)
}

func runChaos(cmd *cobra.Command, _ []string) error {
	cfg := loadConfig()
	policy, err := safetyPolicy(cfg.ChaosMaxDuration, cfg.ChaosMaxTargets)
	if err != nil {
		return err
	}
	if cfg.ChaosEndpoint == "" {
		fmt.Fprintln(cmd.OutOrStdout(), "No CHAOS_ENDPOINT configured. Showing default experiment plan:")
		for i, exp := range chaos.DefaultExperiments() {
			if err := chaos.Validate(exp, policy); err != nil {
				return fmt.Errorf("default experiment %s: %w", exp.Name, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "  %d. %s (%s) — target: %s, duration: %s\n",
				i+1, exp.Name, exp.Kind, exp.Target, exp.Duration)
		}
		fmt.Fprintln(cmd.OutOrStdout(), "\nSet CHAOS_ENDPOINT to inject experiments.")
		return nil
	}

	exp := chaos.NewExperiment("cloudripper-"+chaosKind, chaosKind, chaosTarget, chaosDuration, chaosAction)
	if exp.Parameters == nil {
		exp.Parameters = map[string]any{}
	}
	exp.Parameters["count"] = chaosCount
	if chaosNamespace != "" {
		exp.Parameters["namespace"] = chaosNamespace
	}
	if err := chaos.Validate(exp, policy); err != nil {
		return err
	}

	logger := auditLogger(cfg)
	if cfg.DryRun {
		if err := writeChaosAudit(logger, cfg.DryRun, "inject", exp.Name, "dry-run", "validated"); err != nil {
			return err
		}
		data, err := json.MarshalIndent(exp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Dry-run — would inject experiment:\n%s\n", data)
		return nil
	}

	confirmer, err := newConfirmer(cmd)
	if err != nil {
		return err
	}
	ok, err := confirmer.Confirm(fmt.Sprintf("Inject %s against %s for %s (count %d)?", exp.Kind, exp.Target, exp.Duration, chaosCount))
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(cmd.OutOrStdout(), "Confirmation declined.")
		return writeChaosAudit(logger, false, "inject", exp.Name, "skipped-confirm", "confirmation declined")
	}

	orchestrator := newOrchestrator(cfg.ChaosProvider, cfg.ChaosEndpoint, cfg.ChaosAPIKey)
	ctx := commandContext(cmd)
	result, err := orchestrator.Inject(ctx, exp)
	if err != nil {
		_ = writeChaosAudit(logger, false, "inject", exp.Name, "failed", err.Error())
		return fmt.Errorf("chaos injection failed: %w", err)
	}
	ref := chaos.ExperimentRef(chaos.Provider(cfg.ChaosProvider), exp, result)
	if err := writeChaosAudit(logger, false, "inject", ref, "applied", exp.Kind+" "+exp.Target); err != nil {
		return err
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Chaos injected successfully:\n%s\n", data)

	if !chaosWait {
		return nil
	}
	timeout := 2 * time.Minute
	if parsed, perr := time.ParseDuration(exp.Duration); perr == nil && parsed+30*time.Second > timeout {
		timeout = parsed + 30*time.Second
	}
	status, err := orchestrator.Poll(ctx, ref, 2*time.Second, timeout)
	fmt.Fprintf(cmd.OutOrStdout(), "phase: %s\n", status.Phase)
	if status.Phase == "failed" && chaosRollback {
		if rerr := orchestrator.Rollback(ctx, ref); rerr != nil {
			_ = writeChaosAudit(logger, false, "rollback", ref, "failed", rerr.Error())
			return rerr
		}
		fmt.Fprintf(cmd.OutOrStdout(), "rolled back %s\n", ref)
		if err := writeChaosAudit(logger, false, "rollback", ref, "applied", "aborted after failure"); err != nil {
			return err
		}
	}
	if err != nil {
		return err
	}
	return nil
}

func runChaosControl(cmd *cobra.Command, abort bool) error {
	cfg := loadConfig()
	if cfg.ChaosEndpoint == "" {
		return fmt.Errorf("chaos endpoint not configured (set CHAOS_ENDPOINT)")
	}
	orchestrator := newOrchestrator(cfg.ChaosProvider, cfg.ChaosEndpoint, cfg.ChaosAPIKey)
	ctx := commandContext(cmd)
	logger := auditLogger(cfg)
	if abort {
		if cfg.DryRun {
			fmt.Fprintf(cmd.OutOrStdout(), "Dry-run — would abort %s\n", chaosRef)
			return writeChaosAudit(logger, true, "abort", chaosRef, "dry-run", "validated")
		}
		result, err := orchestrator.Abort(ctx, chaosRef)
		if err != nil {
			_ = writeChaosAudit(logger, false, "abort", chaosRef, "failed", err.Error())
			return err
		}
		if err := writeChaosAudit(logger, false, "abort", chaosRef, "applied", "aborted"); err != nil {
			return err
		}
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s\n", data)
		return nil
	}
	status, err := orchestrator.Status(ctx, chaosRef)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "phase: %s\n", status.Phase)
	if status.Raw != nil {
		data, err := json.MarshalIndent(status.Raw, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s\n", data)
	}
	return nil
}

func safetyPolicy(maxDuration time.Duration, maxTargets int) (chaos.SafetyPolicy, error) {
	policy := chaos.DefaultSafety()
	policy.MaxDuration = maxDuration
	policy.MaxTargets = maxTargets
	policy.AllowProduction = chaosAllowProd
	if chaosMaxDuration != "" {
		parsed, err := time.ParseDuration(chaosMaxDuration)
		if err != nil {
			return policy, fmt.Errorf("max-duration: %w", err)
		}
		policy.MaxDuration = parsed
	}
	return policy, nil
}

func newOrchestrator(provider, endpoint, apiKey string) *chaos.Orchestrator {
	return chaos.NewOrchestrator(chaos.Config{
		Provider: chaos.Provider(provider),
		Endpoint: endpoint,
		APIKey:   apiKey,
	})
}

func writeChaosAudit(logger *audit.Logger, dry bool, action, name, outcome, detail string) error {
	if logger == nil {
		return nil
	}
	return logger.Write(audit.Event{
		Command:    "chaos",
		Action:     action,
		ResourceID: name,
		Outcome:    outcome,
		Detail:     detail,
		DryRun:     dry,
	})
}
