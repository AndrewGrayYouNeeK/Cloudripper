package cmd

import (
	"github.com/AndrewGrayYouNeeK/cloudripper/internal/cloud"
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

func buildProviders(cfg config.Config) []cloud.Provider {
	var providers []cloud.Provider

	// Always include AWS; the SDK credential chain resolves IAM roles, SSO, etc.
	providers = append(providers, cloud.NewAWSProvider(cfg.AWSRegion))

	if cfg.GCPProject != "" {
		providers = append(providers, cloud.NewGCPProvider(cfg.GCPProject, cfg.GCPRegion))
	}

	return providers
}
