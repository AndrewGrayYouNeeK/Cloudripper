package runtime

import (
	"context"
	"fmt"
	"strings"

	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/cloud"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/config"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/cost"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/scale"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/watch"
)

// BuildProviders returns the providers a scan should use.
// AWS is always included. The SDK credential chain resolves profiles, SSO, and roles.
// GCP, Azure, and Cloudflare are included only when their settings are present.
func BuildProviders(cfg config.Config) []cloud.Provider {
	var providers []cloud.Provider

	awsProvider := cloud.NewAWSProvider(cfg.AWSRegion)
	awsProvider.SetAllRegions(cfg.AllRegions)
	providers = append(providers, awsProvider)

	if cfg.GCPProject != "" {
		providers = append(providers, cloud.NewGCPProvider(cfg.GCPProject, cfg.GCPRegion))
	}
	if cfg.AzureSubscription != "" {
		providers = append(providers, cloud.NewAzureProvider(cfg.AzureSubscription, cfg.AzureResourceGroup))
	}
	if cloud.CloudflareConfigured(cfg.CloudflareAccountID, cfg.CloudflareAPIToken) {
		providers = append(providers, cloud.NewCloudflareProvider(cfg.CloudflareAccountID, cfg.CloudflareAPIToken))
	}
	return providers
}

// DefaultScan inventories every configured provider.
// A partial failure still returns the resources that were found.
func DefaultScan(ctx context.Context, cfg config.Config) ([]cloud.Resource, error) {
	providers := BuildProviders(cfg)
	if len(providers) == 0 {
		return nil, fmt.Errorf("no cloud providers configured — set AWS credentials, GCP_PROJECT, AZURE_SUBSCRIPTION_ID, or CLOUDFLARE_ACCOUNT_ID + CLOUDFLARE_API_TOKEN")
	}
	return cloud.NewMultiScanner(providers...).ScanAll(ctx)
}

// DefaultCosts resolves estimates and trailing actuals for a scan.
// The error return is always nil; source failures are warnings on the resolution.
func DefaultCosts(ctx context.Context, cfg config.Config, resources []cloud.Resource) (cost.Resolution, error) {
	var sources []cost.Source
	costRegion := cfg.AWSRegion
	if cfg.AllRegions {
		costRegion = ""
	}
	if explorer, err := cost.NewAWSExplorer(ctx, costRegion); err != nil {
		sources = append(sources, cost.StaticSource{SourceName: cloud.CostSourceAWS, Err: err})
	} else {
		sources = append(sources, explorer)
	}
	if cfg.GCPProject != "" {
		if cfg.GCPBillingTable == "" {
			sources = append(sources, cost.StaticSource{SourceName: cloud.CostSourceGCP, Err: fmt.Errorf("GCP_BILLING_TABLE is not set")})
		} else if queryer, err := cost.NewGCPQueryer(ctx, cfg.GCPProject); err != nil {
			sources = append(sources, cost.StaticSource{SourceName: cloud.CostSourceGCP, Err: err})
		} else {
			sources = append(sources, &cost.GCPBilling{Table: cfg.GCPBillingTable, Query: queryer})
		}
	}
	if cfg.AzureSubscription != "" {
		if azureCost, err := cost.NewAzureCost(cfg.AzureSubscription, cfg.AzureResourceGroup); err != nil {
			sources = append(sources, cost.StaticSource{SourceName: cloud.CostSourceAzure, Err: err})
		} else {
			sources = append(sources, azureCost)
		}
	}
	if cloud.CloudflareConfigured(cfg.CloudflareAccountID, cfg.CloudflareAPIToken) {
		sources = append(sources, cost.NewCloudflareBilling(cfg.CloudflareAccountID, cfg.CloudflareAPIToken))
	}
	return cost.Resolve(ctx, resources, sources), nil
}

// DefaultExecutors returns mutation executors for the CLI.
// The desktop app does not call this.
func DefaultExecutors(_ context.Context, cfg config.Config) ([]cloud.Executor, error) {
	var execs []cloud.Executor
	for _, provider := range BuildProviders(cfg) {
		exec, ok := provider.(cloud.Executor)
		if !ok {
			continue
		}
		execs = append(execs, exec)
	}
	if len(execs) == 0 {
		return nil, fmt.Errorf("no cloud executors configured")
	}
	return execs, nil
}

// DefaultScalers returns scaling controllers for the CLI.
// The desktop app does not call this.
func DefaultScalers(ctx context.Context, cfg config.Config) ([]scale.Controller, error) {
	var controllers []scale.Controller
	if awsCtrl, err := scale.NewAWSController(ctx, cfg.AWSRegion); err == nil {
		controllers = append(controllers, awsCtrl)
	}
	if cfg.GCPProject != "" {
		if gcpCtrl, err := scale.NewGCPController(ctx, cfg.GCPProject, cfg.GCPRegion); err == nil {
			controllers = append(controllers, gcpCtrl)
		}
	}
	if len(controllers) == 0 {
		return nil, fmt.Errorf("no scaling controllers configured")
	}
	return controllers, nil
}

// SkippedProviders returns the same skip lines the CLI prints for unset clouds.
func SkippedProviders(cfg config.Config) []string {
	var lines []string
	if strings.TrimSpace(cfg.GCPProject) == "" {
		lines = append(lines, "skipped: GCP project is not set (set GCP_PROJECT or --gcp-project)")
	}
	if strings.TrimSpace(cfg.AzureSubscription) == "" {
		lines = append(lines, "skipped: Azure subscription is not set (set AZURE_SUBSCRIPTION_ID or --azure-subscription)")
	}
	account := strings.TrimSpace(cfg.CloudflareAccountID)
	token := strings.TrimSpace(cfg.CloudflareAPIToken)
	switch {
	case account == "" && token == "":
		lines = append(lines, "skipped: Cloudflare is not set (set CLOUDFLARE_ACCOUNT_ID and CLOUDFLARE_API_TOKEN)")
	case account == "":
		lines = append(lines, "skipped: Cloudflare account id is not set (set CLOUDFLARE_ACCOUNT_ID or --cloudflare-account)")
	case token == "":
		lines = append(lines, "skipped: Cloudflare API token is not set (set CLOUDFLARE_API_TOKEN)")
	}
	return lines
}

// SnapshotFrom builds a watch sample the same way the CLI does.
// Actual account spend is used when a billing source returned it.
// Otherwise the sample is the sum of priced per-resource estimates.
func SnapshotFrom(resolution cost.Resolution) watch.Snapshot {
	snap := watch.Snapshot{ByProvider: map[string]float64{}}
	if resolution.ActualAvailable {
		snap.Total = resolution.ActualTotal
		for _, item := range resolution.Snapshots {
			switch item.Source {
			case cloud.CostSourceAWS:
				snap.ByProvider["aws"] += item.TotalUSD
			case cloud.CostSourceGCP:
				snap.ByProvider["gcp"] += item.TotalUSD
			case cloud.CostSourceAzure:
				snap.ByProvider["azure"] += item.TotalUSD
			default:
				snap.ByProvider[item.Source] += item.TotalUSD
			}
		}
		return snap
	}
	for _, r := range resolution.Resources {
		snap.Total += r.CostUSD
		snap.ByProvider[r.Provider] += r.CostUSD
	}
	return snap
}
