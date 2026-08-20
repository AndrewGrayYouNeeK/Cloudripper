package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	dryRun    bool
	awsRegion string
	gcpProject string
)

// rootCmd is the base CLI command.
var rootCmd = &cobra.Command{
	Use:   "cloudripper",
	Short: "Multi-cloud orchestration that scales, optimizes, and breaks things on purpose",
	Long: `Cloudripper — multi-cloud orchestration for AWS and GCP.

Scan resources, slash your bill, and run chaos experiments.
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
	rootCmd.PersistentFlags().StringVar(&awsRegion, "aws-region", "", "AWS region (default: AWS_REGION env or us-east-1)")
	rootCmd.PersistentFlags().StringVar(&gcpProject, "gcp-project", "", "GCP project (default: GCP_PROJECT env)")
}
