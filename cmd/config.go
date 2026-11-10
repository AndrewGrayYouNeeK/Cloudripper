package cmd

import (
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/cloud"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/config"
	appruntime "github.com/AndrewGrayYouNeeK/Cloudripper/internal/runtime"
)

func loadConfig() config.Config {
	cfg := config.Load()
	if awsRegion != "" {
		cfg.AWSRegion = awsRegion
	}
	if rootCmd.PersistentFlags().Changed("all-regions") {
		cfg.AllRegions = allRegions
	}
	if gcpProject != "" {
		cfg.GCPProject = gcpProject
	}
	if gcpRegionFlag != "" {
		cfg.GCPRegion = gcpRegionFlag
	}
	if azureSubscription != "" {
		cfg.AzureSubscription = azureSubscription
	}
	if azureResourceGroup != "" {
		cfg.AzureResourceGroup = azureResourceGroup
	}
	if cloudflareAccountID != "" {
		cfg.CloudflareAccountID = cloudflareAccountID
	}
	if auditLog != "" {
		cfg.AuditLog = auditLog
	}
	// Only override env DRY_RUN when the flag was explicitly set.
	if rootCmd.PersistentFlags().Changed("dry-run") {
		cfg.DryRun = dryRun
	}
	return cfg
}

func buildProviders(cfg config.Config) []cloud.Provider {
	return appruntime.BuildProviders(cfg)
}
