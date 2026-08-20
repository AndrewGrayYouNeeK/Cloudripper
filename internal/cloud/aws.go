package cloud

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/rds"
)

// AWSProvider scans AWS resources.
type AWSProvider struct {
	region string
}

// NewAWSProvider creates an AWS scanner for the given region.
func NewAWSProvider(region string) *AWSProvider {
	return &AWSProvider{region: region}
}

func (a *AWSProvider) Name() string { return "aws" }

func (a *AWSProvider) Scan(ctx context.Context) ([]Resource, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(a.region))
	if err != nil {
		return nil, fmt.Errorf("aws config: %w", err)
	}

	var resources []Resource

	ec2Client := ec2.NewFromConfig(cfg)
	instances, err := ec2Client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{})
	if err != nil {
		return nil, fmt.Errorf("aws ec2 scan: %w", err)
	}
	for _, reservation := range instances.Reservations {
		for _, inst := range reservation.Instances {
			name := instanceName(inst.Tags)
			status := string(inst.State.Name)
			resources = append(resources, Resource{
				ID:       aws.ToString(inst.InstanceId),
				Name:     name,
				Provider: "aws",
				Type:     ResourceEC2,
				Region:   a.region,
				Status:   status,
				CostUSD:  estimateEC2Cost(string(inst.InstanceType), status),
				Tags:     tagsToMap(inst.Tags),
			})
		}
	}

	rdsClient := rds.NewFromConfig(cfg)
	dbs, err := rdsClient.DescribeDBInstances(ctx, &rds.DescribeDBInstancesInput{})
	if err != nil {
		return nil, fmt.Errorf("aws rds scan: %w", err)
	}
	for _, db := range dbs.DBInstances {
		resources = append(resources, Resource{
			ID:       aws.ToString(db.DBInstanceIdentifier),
			Name:     aws.ToString(db.DBInstanceIdentifier),
			Provider: "aws",
			Type:     ResourceRDS,
			Region:   a.region,
			Status:   aws.ToString(db.DBInstanceStatus),
			CostUSD:  estimateRDSCost(aws.ToString(db.DBInstanceClass)),
			Tags:     map[string]string{},
		})
	}

	return resources, nil
}

func instanceName(tags []ec2types.Tag) string {
	for _, t := range tags {
		if aws.ToString(t.Key) == "Name" {
			return aws.ToString(t.Value)
		}
	}
	return ""
}

func tagsToMap(tags []ec2types.Tag) map[string]string {
	m := make(map[string]string, len(tags))
	for _, t := range tags {
		m[aws.ToString(t.Key)] = aws.ToString(t.Value)
	}
	return m
}

const stoppedStorageCostFactor = 0.1 // stopped EC2 bills EBS only, ~10% of running compute

func estimateEC2Cost(instanceType, status string) float64 {
	runningCost := ec2RunningCost(instanceType)
	switch strings.ToLower(status) {
	case "stopped":
		// Stopped instances bill for attached EBS storage, not compute.
		return runningCost * stoppedStorageCostFactor
	case "terminated", "shutting-down":
		return 0
	default:
		return runningCost
	}
}

func ec2RunningCost(instanceType string) float64 {
	estimates := map[string]float64{
		"t3.micro":   7.5,
		"t3.small":   15.0,
		"t3.medium":  30.0,
		"m5.large":   70.0,
		"m5.xlarge":  140.0,
		"c5.2xlarge": 248.0,
	}
	if cost, ok := estimates[instanceType]; ok {
		return cost
	}
	return 50.0
}

func estimateRDSCost(instanceClass string) float64 {
	estimates := map[string]float64{
		"db.t3.micro":  12.0,
		"db.t3.small":  24.0,
		"db.t3.medium": 48.0,
		"db.m5.large":  120.0,
	}
	if cost, ok := estimates[instanceClass]; ok {
		return cost
	}
	return 80.0
}
