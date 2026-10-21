package cloud

import (
	"context"
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

// AWSProvider scans and mutates AWS resources in one region.
type AWSProvider struct {
	region  string
	clients AWSClients
	load    func(ctx context.Context, region string) (AWSClients, error)

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

// Name implements Provider.
func (a *AWSProvider) Name() string { return "aws" }

// Provider implements Executor.
func (a *AWSProvider) Provider() string { return "aws" }

// Scan implements Provider.
func (a *AWSProvider) Scan(ctx context.Context) ([]Resource, error) {
	if err := a.ensure(ctx); err != nil {
		return nil, err
	}
	return scanAWS(ctx, a.region, a.clients)
}

// Execute implements Executor.
func (a *AWSProvider) Execute(ctx context.Context, r Resource, action, spec string) (string, error) {
	if IsProtected(r.Tags) {
		return "", fmt.Errorf("refusing to change protected resource %s", r.ID)
	}
	if err := a.ensure(ctx); err != nil {
		return "", err
	}
	return executeAWS(ctx, a.clients, r, action, spec)
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
