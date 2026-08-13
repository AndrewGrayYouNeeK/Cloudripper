package cmd

import (
	"context"
	"fmt"

	"github.com/AndrewGrayYouNeeK/cloudripper/internal/cloud"
	"github.com/AndrewGrayYouNeeK/cloudripper/internal/optimizer"
	"github.com/spf13/cobra"
)

var optimizeCmd = &cobra.Command{
	Use:   "optimize",
	Short: "Analyze cloud spend and recommend cost cuts",
	Long:  "Scans AWS and GCP resources, identifies waste, and suggests rightsizing, scheduling, and termination actions.",
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

		result := optimizer.Analyze(resources)
		fmt.Print(optimizer.FormatReport(result))

		if dryRun {
			fmt.Println("\n(dry-run mode — no changes applied)")
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(optimizeCmd)
}
