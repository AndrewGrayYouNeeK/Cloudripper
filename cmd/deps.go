package cmd

import (
	"context"

	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/cloud"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/config"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/cost"
	appruntime "github.com/AndrewGrayYouNeeK/Cloudripper/internal/runtime"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/scale"
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
	return appruntime.DefaultScan(ctx, cfg)
}

func defaultCosts(ctx context.Context, cfg config.Config, resources []cloud.Resource) (cost.Resolution, error) {
	return appruntime.DefaultCosts(ctx, cfg, resources)
}

func defaultExecutors(ctx context.Context, cfg config.Config) ([]cloud.Executor, error) {
	return appruntime.DefaultExecutors(ctx, cfg)
}

func defaultScalers(ctx context.Context, cfg config.Config) ([]scale.Controller, error) {
	return appruntime.DefaultScalers(ctx, cfg)
}
