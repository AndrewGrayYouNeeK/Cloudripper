package scale

import (
	"context"
	"fmt"
	"time"

	"github.com/AndrewGrayYouNeeK/cloudripper/internal/cloud"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

// ASGAPI lists and resizes Auto Scaling groups.
type ASGAPI interface {
	DescribeAutoScalingGroups(ctx context.Context, params *autoscaling.DescribeAutoScalingGroupsInput, optFns ...func(*autoscaling.Options)) (*autoscaling.DescribeAutoScalingGroupsOutput, error)
	SetDesiredCapacity(ctx context.Context, params *autoscaling.SetDesiredCapacityInput, optFns ...func(*autoscaling.Options)) (*autoscaling.SetDesiredCapacityOutput, error)
}

// CloudWatchAPI reads metric statistics.
type CloudWatchAPI interface {
	GetMetricStatistics(ctx context.Context, params *cloudwatch.GetMetricStatisticsInput, optFns ...func(*cloudwatch.Options)) (*cloudwatch.GetMetricStatisticsOutput, error)
}

// AWSController scales Auto Scaling groups and individual EC2 instances.
type AWSController struct {
	Region string
	ASG    ASGAPI
	CW     CloudWatchAPI
	EC2    cloud.EC2API
	Now    func() time.Time
}

// NewAWSController loads AWS clients for the region.
func NewAWSController(ctx context.Context, region string) (*AWSController, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, err
	}
	return &AWSController{
		Region: region,
		ASG:    autoscaling.NewFromConfig(cfg),
		CW:     cloudwatch.NewFromConfig(cfg),
		EC2:    ec2.NewFromConfig(cfg),
	}, nil
}

// Provider implements Controller.
func (a *AWSController) Provider() string { return "aws" }

// Discover lists Auto Scaling groups.
func (a *AWSController) Discover(ctx context.Context) ([]Group, error) {
	if a.ASG == nil {
		return nil, fmt.Errorf("autoscaling client not configured")
	}
	var token *string
	var groups []Group
	for {
		out, err := a.ASG.DescribeAutoScalingGroups(ctx, &autoscaling.DescribeAutoScalingGroupsInput{NextToken: token})
		if err != nil {
			return groups, err
		}
		for _, g := range out.AutoScalingGroups {
			groups = append(groups, groupFromASG(a.Region, g.AutoScalingGroupName, g.DesiredCapacity, g.MinSize, g.MaxSize))
		}
		if out.NextToken == nil || aws.ToString(out.NextToken) == "" {
			return groups, nil
		}
		token = out.NextToken
	}
}

// Lookup finds one ASG or EC2 instance.
func (a *AWSController) Lookup(ctx context.Context, kind, name string) (Group, error) {
	switch kind {
	case KindASG:
		out, err := a.ASG.DescribeAutoScalingGroups(ctx, &autoscaling.DescribeAutoScalingGroupsInput{
			AutoScalingGroupNames: []string{name},
		})
		if err != nil {
			return Group{}, err
		}
		if len(out.AutoScalingGroups) == 0 {
			return Group{}, fmt.Errorf("auto scaling group %s not found", name)
		}
		g := out.AutoScalingGroups[0]
		return groupFromASG(a.Region, g.AutoScalingGroupName, g.DesiredCapacity, g.MinSize, g.MaxSize), nil
	case KindEC2:
		out, err := a.EC2.DescribeInstances(ctx, &ec2.DescribeInstancesInput{InstanceIds: []string{name}})
		if err != nil {
			return Group{}, err
		}
		for _, res := range out.Reservations {
			for _, inst := range res.Instances {
				return Group{
					Provider: "aws",
					Kind:     KindEC2,
					Name:     aws.ToString(inst.InstanceId),
					Region:   a.Region,
					Desired:  1,
					Min:      1,
					Max:      1,
					Spec:     string(inst.InstanceType),
				}, nil
			}
		}
		return Group{}, fmt.Errorf("instance %s not found", name)
	default:
		return Group{}, fmt.Errorf("aws scale kind %q is unsupported", kind)
	}
}

// CPU reads average CPU utilization over the last 15 minutes.
func (a *AWSController) CPU(ctx context.Context, g Group) (float64, error) {
	if a.CW == nil {
		return 0, fmt.Errorf("cloudwatch client not configured")
	}
	now := time.Now()
	if a.Now != nil {
		now = a.Now()
	}
	dimName := "AutoScalingGroupName"
	if g.Kind == KindEC2 {
		dimName = "InstanceId"
	}
	out, err := a.CW.GetMetricStatistics(ctx, &cloudwatch.GetMetricStatisticsInput{
		Namespace:  aws.String("AWS/EC2"),
		MetricName: aws.String("CPUUtilization"),
		Dimensions: []cwtypes.Dimension{{
			Name:  aws.String(dimName),
			Value: aws.String(g.Name),
		}},
		StartTime:  aws.Time(now.Add(-15 * time.Minute)),
		EndTime:    aws.Time(now),
		Period:     aws.Int32(300),
		Statistics: []cwtypes.Statistic{cwtypes.StatisticAverage},
	})
	if err != nil {
		return 0, err
	}
	values := make([]float64, 0, len(out.Datapoints))
	for _, point := range out.Datapoints {
		if point.Average != nil {
			values = append(values, *point.Average)
		}
	}
	return Average(values)
}

// Apply resizes an ASG or an EC2 instance.
func (a *AWSController) Apply(ctx context.Context, g Group, decision Decision) error {
	switch decision.Action {
	case ActionNone:
		return nil
	case ActionScaleOut, ActionScaleIn:
		_, err := a.ASG.SetDesiredCapacity(ctx, &autoscaling.SetDesiredCapacityInput{
			AutoScalingGroupName: aws.String(g.Name),
			DesiredCapacity:      aws.Int32(int32(decision.To)),
			HonorCooldown:        aws.Bool(false),
		})
		return err
	case ActionResizeUp, ActionResizeDown:
		var spec string
		var ok bool
		if decision.Action == ActionResizeUp {
			spec, ok = cloud.LargerSpec("aws", g.Spec)
		} else {
			spec, ok = cloud.SmallerSpec("aws", g.Spec)
		}
		if !ok {
			return fmt.Errorf("no adjacent size for %s", g.Spec)
		}
		_, err := cloud.ResizeEC2(ctx, a.EC2, g.Name, spec)
		return err
	default:
		return fmt.Errorf("unsupported scale action %s", decision.Action)
	}
}

func groupFromASG(region string, name *string, desired, min, max *int32) Group {
	return Group{
		Provider: "aws",
		Kind:     KindASG,
		Name:     aws.ToString(name),
		Region:   region,
		Desired:  int(aws.ToInt32(desired)),
		Min:      int(aws.ToInt32(min)),
		Max:      int(aws.ToInt32(max)),
	}
}
