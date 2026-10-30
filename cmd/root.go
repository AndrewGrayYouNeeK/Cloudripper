package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	dryRun             bool
	yes                bool
	outputFmt          string
	awsRegion          string
	gcpProject         string
	gcpRegionFlag      string
	azureSubscription  string
	azureResourceGroup string
	auditLog           string
	allRegions         bool
)

// rootCmd is the base CLI command.
var rootCmd = &cobra.Command{
	Use:   "cloudripper",
	Short: "Multi-cloud orchestration that scales, optimizes, and breaks things on purpose",
	Long: `Cloudripper — multi-cloud orchestration for AWS, GCP, and Azure.

Scan resources, pull actual spend when billing APIs are available, apply cost
actions you confirm, scale instance groups, watch for cost spikes, and run
chaos experiments with a blast-radius limit.
No third-party backends. Just your clouds and this CLI.`,
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false, "preview actions without making changes")
	rootCmd.PersistentFlags().BoolVar(&yes, "yes", false, "skip interactive confirmation for apply, scale, schedule, and chaos")
	rootCmd.PersistentFlags().StringVar(&outputFmt, "output", "", "output format: table, json, or csv (default table)")
	rootCmd.PersistentFlags().StringVar(&awsRegion, "aws-region", "", "AWS region (default: AWS_REGION env or us-east-1)")
	rootCmd.PersistentFlags().BoolVar(&allRegions, "all-regions", false, "scan every enabled AWS region; opt-in regions that are not enabled are skipped")
	rootCmd.PersistentFlags().StringVar(&gcpProject, "gcp-project", "", "GCP project (default: GCP_PROJECT env)")
	rootCmd.PersistentFlags().StringVar(&gcpRegionFlag, "gcp-region", "", "GCP region (default: GCP_REGION env or us-central1)")
	rootCmd.PersistentFlags().StringVar(&azureSubscription, "azure-subscription", "", "Azure subscription ID (default: AZURE_SUBSCRIPTION_ID)")
	rootCmd.PersistentFlags().StringVar(&azureResourceGroup, "azure-resource-group", "", "optional Azure resource group filter")
	rootCmd.PersistentFlags().StringVar(&auditLog, "audit-log", "", "append-only JSONL audit log (default: CLOUDRIPPER_AUDIT_LOG or cloudripper-audit.log)")
}
