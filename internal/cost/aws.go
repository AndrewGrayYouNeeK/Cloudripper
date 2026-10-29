package cost

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/cloud"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	cetypes "github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
)

// CostExplorerAPI is the Cost Explorer surface CloudRipper uses.
type CostExplorerAPI interface {
	GetCostAndUsage(ctx context.Context, params *costexplorer.GetCostAndUsageInput, optFns ...func(*costexplorer.Options)) (*costexplorer.GetCostAndUsageOutput, error)
	GetCostAndUsageWithResources(ctx context.Context, params *costexplorer.GetCostAndUsageWithResourcesInput, optFns ...func(*costexplorer.Options)) (*costexplorer.GetCostAndUsageWithResourcesOutput, error)
}

// AWSExplorer reads trailing-window spend for one region.
type AWSExplorer struct {
	API    CostExplorerAPI
	Region string
	Now    func() time.Time
}

// NewAWSExplorer loads credentials and a Cost Explorer client (the API is hosted in us-east-1).
func NewAWSExplorer(ctx context.Context, region string) (*AWSExplorer, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion("us-east-1"))
	if err != nil {
		return nil, err
	}
	return &AWSExplorer{API: costexplorer.NewFromConfig(cfg), Region: region}, nil
}

// Name implements Source.
func (a *AWSExplorer) Name() string { return cloud.CostSourceAWS }

// Fetch implements Source.
func (a *AWSExplorer) Fetch(ctx context.Context) (Snapshot, error) {
	if a == nil || a.API == nil {
		return Snapshot{}, fmt.Errorf("cost explorer client not configured")
	}
	now := time.Now
	if a.Now != nil {
		now = a.Now
	}
	start, end := Window(now())
	snap := Snapshot{Source: cloud.CostSourceAWS}
	services, err := a.API.GetCostAndUsage(ctx, usageInput(a.Region, start, end, "SERVICE"))
	if err != nil {
		return Snapshot{}, fmt.Errorf("%s", FormatCostExplorerError(err))
	}
	svcCosts, total, warnings := totalsFromResults(services.ResultsByTime)
	snap.Services = svcCosts
	snap.TotalUSD = total
	snap.Warnings = append(snap.Warnings, warnings...)

	resources, err := a.API.GetCostAndUsageWithResources(ctx, &costexplorer.GetCostAndUsageWithResourcesInput{
		TimePeriod:  dateInterval(start, end),
		Granularity: cetypes.GranularityDaily,
		Metrics:     []string{"UnblendedCost"},
		GroupBy: []cetypes.GroupDefinition{{
			Type: cetypes.GroupDefinitionTypeDimension,
			Key:  aws.String("RESOURCE_ID"),
		}},
		Filter: regionFilter(a.Region),
	})
	if err != nil {
		if costExplorerDisabled(err.Error()) {
			snap.Warnings = append(snap.Warnings, FormatCostExplorerError(err))
		} else {
			snap.Warnings = append(snap.Warnings, "resource-level cost explorer data unavailable: "+err.Error())
		}
		return snap, nil
	}
	for _, period := range resources.ResultsByTime {
		for _, group := range period.Groups {
			amount, unit := metricAmount(group.Metrics)
			usd, err := ParseUSD(amount, unit)
			if err != nil {
				snap.Warnings = append(snap.Warnings, err.Error())
				continue
			}
			id := ""
			if len(group.Keys) > 0 {
				id = group.Keys[0]
			}
			if id == "" {
				continue
			}
			snap.Resources = append(snap.Resources, ResourceCost{ResourceID: id, USD: usd})
		}
	}
	return snap, nil
}

func usageInput(region string, start, end time.Time, key string) *costexplorer.GetCostAndUsageInput {
	return &costexplorer.GetCostAndUsageInput{
		TimePeriod:  dateInterval(start, end),
		Granularity: cetypes.GranularityDaily,
		Metrics:     []string{"UnblendedCost"},
		GroupBy: []cetypes.GroupDefinition{{
			Type: cetypes.GroupDefinitionTypeDimension,
			Key:  aws.String(key),
		}},
		Filter: regionFilter(region),
	}
}

func dateInterval(start, end time.Time) *cetypes.DateInterval {
	return &cetypes.DateInterval{
		Start: aws.String(start.Format("2006-01-02")),
		End:   aws.String(end.Format("2006-01-02")),
	}
}

func regionFilter(region string) *cetypes.Expression {
	if region == "" {
		return nil
	}
	return &cetypes.Expression{
		Dimensions: &cetypes.DimensionValues{
			Key:    cetypes.DimensionRegion,
			Values: []string{region},
		},
	}
}

func totalsFromResults(periods []cetypes.ResultByTime) ([]ServiceCost, float64, []string) {
	var services []ServiceCost
	var total float64
	var warnings []string
	for _, period := range periods {
		for _, group := range period.Groups {
			amount, unit := metricAmount(group.Metrics)
			usd, err := ParseUSD(amount, unit)
			if err != nil {
				warnings = append(warnings, err.Error())
				continue
			}
			name := ""
			if len(group.Keys) > 0 {
				name = group.Keys[0]
			}
			services = append(services, ServiceCost{Service: name, USD: usd})
			total += usd
		}
	}
	return services, total, warnings
}

func metricAmount(metrics map[string]cetypes.MetricValue) (string, string) {
	metric, ok := metrics["UnblendedCost"]
	if !ok {
		return "0", "USD"
	}
	return aws.ToString(metric.Amount), aws.ToString(metric.Unit)
}

// ParseAmount is exported for tests that already hold a raw string.
func ParseAmount(amount string) (float64, error) {
	return strconv.ParseFloat(amount, 64)
}

// FormatCostExplorerError replaces the raw "not enabled for Cost Explorer"
// API error with setup guidance. Other errors are returned unchanged.
func FormatCostExplorerError(err error) string {
	if err == nil {
		return ""
	}
	if costExplorerDisabled(err.Error()) {
		return "Cost Explorer is not enabled for this account. Enable it in the AWS Billing console; cost data usually appears after about 24 hours."
	}
	return err.Error()
}

func costExplorerDisabled(msg string) bool {
	return strings.Contains(strings.ToLower(msg), "not enabled for cost explorer")
}
