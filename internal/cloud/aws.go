package cloud

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// AWSProvider scans and mutates AWS resources in one region, or every enabled
// region when AllRegions is set.
type AWSProvider struct {
	region     string
	allRegions bool
	clients    AWSClients
	load       func(ctx context.Context, region string) (AWSClients, error)
	byRegion   map[string]AWSClients

	mu      sync.Mutex
	loaded  bool
	loadErr error
}

// NewAWSProvider creates an AWS provider that loads the default credential chain on first use.
func NewAWSProvider(region string) *AWSProvider {
	return &AWSProvider{region: region, load: loadAWSClients}
}

// NewAWSProviderWithClients creates a provider backed by the given clients (tests).
func NewAWSProviderWithClients(region string, clients AWSClients) *AWSProvider {
	return &AWSProvider{region: region, clients: clients, loaded: true}
}

// SetAllRegions scans every enabled region instead of Region alone.
// Opt-in regions that are not enabled are skipped.
func (a *AWSProvider) SetAllRegions(enabled bool) {
	if a == nil {
		return
	}
	a.allRegions = enabled
}

// Name implements Provider.
func (a *AWSProvider) Name() string { return "aws" }

// Provider implements Executor.
func (a *AWSProvider) Provider() string { return "aws" }

// Scan implements Provider.
func (a *AWSProvider) Scan(ctx context.Context) ([]Resource, error) {
	if a.allRegions {
		return a.scanAllRegions(ctx)
	}
	if err := a.ensure(ctx); err != nil {
		return nil, err
	}
	return scanAWS(ctx, a.region, a.clients)
}

func (a *AWSProvider) scanAllRegions(ctx context.Context) ([]Resource, error) {
	seed, err := a.clientsFor(ctx, a.region)
	if err != nil {
		return nil, err
	}
	regions, err := EnabledRegions(ctx, seed.EC2)
	if err != nil {
		return nil, fmt.Errorf("describe regions: %w", err)
	}
	var resources []Resource
	var errs []error
	s3Listed := false
	for _, region := range regions {
		clients, err := a.clientsFor(ctx, region)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", region, err))
			continue
		}
		clients.S3 = nil
		got, err := scanAWS(ctx, region, clients)
		resources = append(resources, got...)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", region, err))
		}
		if !s3Listed && seed.S3 != nil {
			s3Listed = true
			buckets, s3err := scanS3(ctx, seed.S3, "")
			resources = append(resources, buckets...)
			if s3err != nil {
				errs = append(errs, fmt.Errorf("s3: %w", s3err))
			}
		}
	}
	return resources, errorsJoin(errs)
}

// Execute implements Executor.
func (a *AWSProvider) Execute(ctx context.Context, r Resource, action, spec string) (string, error) {
	if IsProtected(r.Tags) {
		return "", fmt.Errorf("refusing to change protected resource %s", r.ID)
	}
	if a.allRegions {
		region := a.region
		if r.Region != "" {
			region = r.Region
		}
		clients, err := a.clientsFor(ctx, region)
		if err != nil {
			return "", err
		}
		return executeAWS(ctx, clients, r, action, spec)
	}
	if err := a.ensure(ctx); err != nil {
		return "", err
	}
	return executeAWS(ctx, a.clients, r, action, spec)
}

func (a *AWSProvider) clientsFor(ctx context.Context, region string) (AWSClients, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.byRegion == nil {
		a.byRegion = map[string]AWSClients{}
	}
	if clients, ok := a.byRegion[region]; ok {
		return clients, nil
	}
	if a.loaded && region == a.region && (a.clients.EC2 != nil || a.clients.S3 != nil) {
		a.byRegion[region] = a.clients
		return a.clients, nil
	}
	if a.load == nil {
		return AWSClients{}, fmt.Errorf("aws clients not configured")
	}
	clients, err := a.load(ctx, region)
	if err != nil {
		return AWSClients{}, fmt.Errorf("aws config: %w", err)
	}
	a.byRegion[region] = clients
	return clients, nil
}

func errorsJoin(errs []error) error {
	return errors.Join(errs...)
}

func (a *AWSProvider) ensure(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.loaded {
		return a.loadErr
	}
	if a.load == nil {
		a.loaded = true
		a.loadErr = fmt.Errorf("aws clients not configured")
		return a.loadErr
	}
	clients, err := a.load(ctx, a.region)
	a.loaded = true
	if err != nil {
		a.loadErr = fmt.Errorf("aws config: %w", err)
		return a.loadErr
	}
	a.clients = clients
	return nil
}

func loadAWSClients(ctx context.Context, region string) (AWSClients, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return AWSClients{}, err
	}
	return AWSClients{
		EC2:    ec2.NewFromConfig(cfg),
		RDS:    rds.NewFromConfig(cfg),
		S3:     s3.NewFromConfig(cfg),
		ELB:    elasticloadbalancingv2.NewFromConfig(cfg),
		Lambda: lambda.NewFromConfig(cfg),
		EKS:    eks.NewFromConfig(cfg),
		ASG:    autoscaling.NewFromConfig(cfg),
	}, nil
}

// Clients returns the loaded clients after ensure. Scale and cost adapters use this
// only through their own constructors; tests use NewAWSProviderWithClients.
func (a *AWSProvider) Clients() AWSClients {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.clients
}

func awsString(v *string) string {
	return aws.ToString(v)
}
