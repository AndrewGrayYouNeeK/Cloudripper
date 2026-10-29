package cmd

import (
	"context"
	"fmt"

	"github.com/AndrewGrayYouNeeK/cloudripper/internal/cloud"
	"github.com/AndrewGrayYouNeeK/cloudripper/internal/config"
	"github.com/AndrewGrayYouNeeK/cloudripper/internal/cost"
	"github.com/AndrewGrayYouNeeK/cloudripper/internal/scale"
)

type runtime struct {
	scan      func(context.Context, config.Config) ([]cloud.Resource, error)
	costs     func(context.Context, config.Config, []cloud.Resource) (cost.Resolution, error)
	executors func(context.Context, config.Config) ([]cloud.Executor, error)
	scalers   func(context.Context, config.Config) ([]scale.Controller, error)
}

var rt = defaultRuntime()

func defaultRuntime() runtime {
	return runtime{
		scan:      defaultScan,
		costs:     defaultCosts,
		executors: defaultExecutors,
		scalers:   defaultScalers,
	}
}

func defaultScan(ctx context.Context, cfg config.Config) ([]cloud.Resource, error) {
	providers := buildProviders(cfg)
	if len(providers) == 0 {
		return nil, fmt.Errorf("no cloud providers configured — set AWS credentials, GCP_PROJECT, or AZURE_SUBSCRIPTION_ID")
	}
	return cloud.NewMultiScanner(providers...).ScanAll(ctx)
}

func defaultCosts(ctx context.Context, cfg config.Config, resources []cloud.Resource) (cost.Resolution, error) {
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
	return cost.Resolve(ctx, resources, sources), nil
}

func defaultExecutors(_ context.Context, cfg config.Config) ([]cloud.Executor, error) {
	var execs []cloud.Executor
	for _, provider := range buildProviders(cfg) {
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

func defaultScalers(ctx context.Context, cfg config.Config) ([]scale.Controller, error) {
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
