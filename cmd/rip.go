package cmd

import (
	"context"
	"fmt"

	"github.com/AndrewGrayYouNeeK/cloudripper/internal/cloud"
	"github.com/AndrewGrayYouNeeK/cloudripper/internal/config"
	"github.com/spf13/cobra"
)

var ripCmd = &cobra.Command{
	Use:   "rip",
	Short: "Scan multi-cloud resources and report what's running",
	Long:  "Connects to AWS and GCP, discovers compute and database resources, and prints a summary.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := loadConfig()
		ctx := context.Background()

		providers := buildProviders(cfg)
		if len(providers) == 0 {
			return fmt.Errorf("no cloud providers configured — set AWS credentials or GCP_PROJECT")
		}

		scanner := cloud.NewMultiScanner(providers...)
		resources, err := scanner.ScanAll(ctx)
		if err != nil {
			return fmt.Errorf("scan failed: %w", err)
		}

		if len(resources) == 0 {
			fmt.Println("No resources found. Either your clouds are empty or credentials need setup.")
			return nil
		}

		var totalCost float64
		fmt.Printf("Found %d resources across %d provider(s):\n\n", len(resources), len(providers))
		for _, r := range resources {
			totalCost += r.CostUSD
			name := r.Name
			if name == "" {
				name = r.ID
			}
			fmt.Printf("  [%s] %s/%s — %s — $%.2f/mo\n",
				r.Provider, r.Type, name, r.Status, r.CostUSD)
		}
		fmt.Printf("\nEstimated monthly spend: $%.2f\n", totalCost)

		if cfg.DryRun {
			fmt.Println("\n(dry-run mode — no changes made)")
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(ripCmd)
}

func buildProviders(cfg config.Config) []cloud.Provider {
	var providers []cloud.Provider

	// Always include AWS; the SDK credential chain resolves IAM roles, SSO, etc.
	providers = append(providers, cloud.NewAWSProvider(cfg.AWSRegion))

	if cfg.GCPProject != "" {
		providers = append(providers, cloud.NewGCPProvider(cfg.GCPProject, cfg.GCPRegion))
	}

	return providers
}
