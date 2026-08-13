package cmd

import (
	"context"
	"fmt"
	"os"

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

		if dryRun {
			fmt.Println("\n(dry-run mode — no changes made)")
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(ripCmd)
}

func loadConfig() config.Config {
	cfg := config.Load()
	if awsRegion != "" {
		cfg.AWSRegion = awsRegion
	}
	if gcpProject != "" {
		cfg.GCPProject = gcpProject
	}
	cfg.DryRun = dryRun
	return cfg
}

func buildProviders(cfg config.Config) []cloud.Provider {
	var providers []cloud.Provider

	if hasAWSCredentials() {
		providers = append(providers, cloud.NewAWSProvider(cfg.AWSRegion))
	}
	if cfg.GCPProject != "" {
		providers = append(providers, cloud.NewGCPProvider(cfg.GCPProject, cfg.GCPRegion))
	}

	return providers
}

func hasAWSCredentials() bool {
	if os.Getenv("AWS_ACCESS_KEY_ID") != "" {
		return true
	}
	if os.Getenv("AWS_PROFILE") != "" {
		return true
	}
	if _, err := os.Stat(os.Getenv("HOME") + "/.aws/credentials"); err == nil {
		return true
	}
	return false
}
