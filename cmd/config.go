package cmd

import (
	"github.com/AndrewGrayYouNeeK/cloudripper/internal/config"
)

func loadConfig() config.Config {
	cfg := config.Load()
	if awsRegion != "" {
		cfg.AWSRegion = awsRegion
	}
	if gcpProject != "" {
		cfg.GCPProject = gcpProject
	}
	// Only override env DRY_RUN when the flag was explicitly set.
	if rootCmd.PersistentFlags().Changed("dry-run") {
		cfg.DryRun = dryRun
	}
	return cfg
}
