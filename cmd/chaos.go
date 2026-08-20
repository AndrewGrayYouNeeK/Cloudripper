package cmd

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/AndrewGrayYouNeeK/cloudripper/internal/chaos"
	"github.com/spf13/cobra"
)

var (
	chaosTarget   string
	chaosDuration string
	chaosKind     string
	chaosAction   string
)

var chaosCmd = &cobra.Command{
	Use:   "chaos",
	Short: "Inject chaos experiments via Chaos Mesh or Gremlin",
	Long:  "Unleash controlled failures against your infrastructure to validate resilience.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := loadConfig()
		ctx := context.Background()

		orchestrator := chaos.NewOrchestrator(chaos.Config{
			Provider: chaos.Provider(cfg.ChaosProvider),
			Endpoint: cfg.ChaosEndpoint,
			APIKey:   cfg.ChaosAPIKey,
		})

		if cfg.ChaosEndpoint == "" {
			fmt.Println("No CHAOS_ENDPOINT configured. Showing default experiment plan:")
			for i, exp := range chaos.DefaultExperiments() {
				fmt.Printf("  %d. %s (%s) — target: %s, duration: %s\n",
					i+1, exp.Name, exp.Kind, exp.Target, exp.Duration)
			}
			fmt.Println("\nSet CHAOS_ENDPOINT to inject experiments.")
			return nil
		}

		action := chaosAction
		if action == "" {
			action = chaos.DefaultActionForKind(chaosKind)
		}

		exp := chaos.Experiment{
			Name:     "cloudripper-" + chaosKind,
			Kind:     chaosKind,
			Target:   chaosTarget,
			Duration: chaosDuration,
			Parameters: map[string]any{
				"action": action,
			},
		}

		if cfg.DryRun {
			data, _ := json.MarshalIndent(exp, "", "  ")
			fmt.Printf("Dry-run — would inject experiment:\n%s\n", data)
			return nil
		}

		result, err := orchestrator.Inject(ctx, exp)
		if err != nil {
			return fmt.Errorf("chaos injection failed: %w", err)
		}

		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Printf("Chaos injected successfully:\n%s\n", data)
		return nil
	},
}

func init() {
	chaosCmd.Flags().StringVar(&chaosTarget, "target", "cloudripper", "target application label")
	chaosCmd.Flags().StringVar(&chaosDuration, "duration", "30s", "experiment duration")
	chaosCmd.Flags().StringVar(&chaosKind, "kind", "PodChaos", "chaos experiment kind")
	chaosCmd.Flags().StringVar(&chaosAction, "action", "", "chaos action (defaults based on kind, e.g. pod-kill)")
	rootCmd.AddCommand(chaosCmd)
}
