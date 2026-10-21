package cloud

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	astypes "github.com/aws/aws-sdk-go-v2/service/autoscaling/types"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func scanAWS(ctx context.Context, region string, c AWSClients) ([]Resource, error) {
	if c.EC2 == nil && c.RDS == nil && c.S3 == nil && c.ELB == nil && c.Lambda == nil && c.EKS == nil && c.ASG == nil {
		return nil, fmt.Errorf("aws clients not configured")
	}
	var resources []Resource
	var errs []error
	volumesListed := c.EC2 != nil

	if c.EC2 != nil {
		got, err := scanEC2Instances(ctx, region, c.EC2, volumesListed)
		resources = append(resources, got...)
		if err != nil {
			errs = append(errs, fmt.Errorf("ec2: %w", err))
		}
		got, err = scanEBS(ctx, region, c.EC2)
		resources = append(resources, got...)
		if err != nil {
			errs = append(errs, fmt.Errorf("ebs: %w", err))
		}
		got, err = scanAddresses(ctx, region, c.EC2)
		resources = append(resources, got...)
		if err != nil {
			errs = append(errs, fmt.Errorf("eip: %w", err))
		}
	}
	if c.RDS != nil {
		got, err := scanRDS(ctx, region, c.RDS)
		resources = append(resources, got...)
		if err != nil {
			errs = append(errs, fmt.Errorf("rds: %w", err))
		}
	}
	if c.S3 != nil {
		got, err := scanS3(ctx, c.S3)
		resources = append(resources, got...)
		if err != nil {
			errs = append(errs, fmt.Errorf("s3: %w", err))
		}
	}
	if c.ELB != nil {
		got, err := scanELB(ctx, region, c.ELB)
		resources = append(resources, got...)
		if err != nil {
			errs = append(errs, fmt.Errorf("elb: %w", err))
		}
	}
	if c.Lambda != nil {
		got, err := scanLambda(ctx, region, c.Lambda)
		resources = append(resources, got...)
		if err != nil {
			errs = append(errs, fmt.Errorf("lambda: %w", err))
		}
	}
	if c.EKS != nil {
		got, err := scanEKS(ctx, region, c.EKS)
		resources = append(resources, got...)
		if err != nil {
			errs = append(errs, fmt.Errorf("eks: %w", err))
		}
	}
	if c.ASG != nil {
		got, err := scanASG(ctx, region, c.ASG)
		resources = append(resources, got...)
		if err != nil {
			errs = append(errs, fmt.Errorf("asg: %w", err))
		}
	}
	return resources, errors.Join(errs...)
}

func scanEC2Instances(ctx context.Context, region string, api EC2API, volumesListed bool) ([]Resource, error) {
	var token *string
	var resources []Resource
	for {
		out, err := api.DescribeInstances(ctx, &ec2.DescribeInstancesInput{NextToken: token})
		if err != nil {
			return resources, err
		}
		for _, reservation := range out.Reservations {
			for _, inst := range reservation.Instances {
				status := ""
				if inst.State != nil {
					status = string(inst.State.Name)
				}
				instanceType := string(inst.InstanceType)
				resources = append(resources, Resource{
					ID:         awsString(inst.InstanceId),
					Name:       instanceName(inst.Tags),
					Provider:   "aws",
					Type:       ResourceEC2,
					Region:     region,
					Status:     status,
					Spec:       instanceType,
					CostUSD:    EC2ScanCost(instanceType, status, volumesListed),
					CostSource: CostSourceEstimate,
					Tags:       tagsToMap(inst.Tags),
				})
			}
		}
		if awsString(out.NextToken) == "" {
			return resources, nil
		}
		token = out.NextToken
	}
}

func scanEBS(ctx context.Context, region string, api EC2API) ([]Resource, error) {
	var token *string
	var resources []Resource
	for {
		out, err := api.DescribeVolumes(ctx, &ec2.DescribeVolumesInput{NextToken: token})
		if err != nil {
			return resources, err
		}
		for _, vol := range out.Volumes {
			size := int64(aws.ToInt32(vol.Size))
			status := string(vol.State)
			resources = append(resources, Resource{
				ID:         awsString(vol.VolumeId),
				Name:       instanceName(vol.Tags),
				Provider:   "aws",
				Type:       ResourceEBS,
				Region:     firstNonEmpty(awsString(vol.AvailabilityZone), region),
				Status:     status,
				Spec:       string(vol.VolumeType),
				CostUSD:    EBSMonthly(string(vol.VolumeType), size),
				CostSource: CostSourceEstimate,
				Tags:       tagsToMap(vol.Tags),
				Metadata:   map[string]string{"size_gb": fmt.Sprintf("%d", size)},
			})
		}
		if awsString(out.NextToken) == "" {
			return resources, nil
		}
		token = out.NextToken
	}
}

func scanAddresses(ctx context.Context, region string, api EC2API) ([]Resource, error) {
	out, err := api.DescribeAddresses(ctx, &ec2.DescribeAddressesInput{})
	if err != nil {
		return nil, err
	}
	var resources []Resource
	for _, addr := range out.Addresses {
		status := "in-use"
		cost := 0.0
		if awsString(addr.AssociationId) == "" {
			status = "unattached"
			cost = EIPUnattachedMonthly
		}
		resources = append(resources, Resource{
			ID:         awsString(addr.AllocationId),
			Name:       awsString(addr.PublicIp),
			Provider:   "aws",
			Type:       ResourceEIP,
			Region:     region,
			Status:     status,
			CostUSD:    cost,
			CostSource: CostSourceEstimate,
			Tags:       tagsToMap(addr.Tags),
			Metadata:   map[string]string{"public_ip": awsString(addr.PublicIp)},
		})
	}
	return resources, nil
}

func scanRDS(ctx context.Context, region string, api RDSAPI) ([]Resource, error) {
	var marker *string
	var resources []Resource
	for {
		out, err := api.DescribeDBInstances(ctx, &rds.DescribeDBInstancesInput{Marker: marker})
		if err != nil {
			return resources, err
		}
		for _, db := range out.DBInstances {
			class := awsString(db.DBInstanceClass)
			resources = append(resources, Resource{
				ID:         awsString(db.DBInstanceIdentifier),
				Name:       awsString(db.DBInstanceIdentifier),
				Provider:   "aws",
				Type:       ResourceRDS,
				Region:     firstNonEmpty(awsString(db.AvailabilityZone), region),
				Status:     awsString(db.DBInstanceStatus),
				Spec:       class,
				CostUSD:    estimateRDSCost(class),
				CostSource: CostSourceEstimate,
				Tags:       rdsTags(db.TagList),
			})
		}
		if awsString(out.Marker) == "" {
			return resources, nil
		}
		marker = out.Marker
	}
}

func scanS3(ctx context.Context, api S3API) ([]Resource, error) {
	var token *string
	var resources []Resource
	for {
		out, err := api.ListBuckets(ctx, &s3.ListBucketsInput{ContinuationToken: token})
		if err != nil {
			return resources, err
		}
		for _, bucket := range out.Buckets {
			name := awsString(bucket.Name)
			tags := map[string]string{}
			if tagOut, tagErr := api.GetBucketTagging(ctx, &s3.GetBucketTaggingInput{Bucket: bucket.Name}); tagErr == nil {
				tags = s3Tags(tagOut.TagSet)
			}
			resources = append(resources, Resource{
				ID:         name,
				Name:       name,
				Provider:   "aws",
				Type:       ResourceS3,
				Region:     awsString(bucket.BucketRegion),
				Status:     "listed",
				CostUSD:    0,
				CostSource: CostSourceEstimate,
				Tags:       tags,
			})
		}
		if awsString(out.ContinuationToken) == "" {
			return resources, nil
		}
		token = out.ContinuationToken
	}
}

func scanELB(ctx context.Context, region string, api ELBAPI) ([]Resource, error) {
	counts, countErr := targetCounts(ctx, api)
	var marker *string
	var resources []Resource
	for {
		out, err := api.DescribeLoadBalancers(ctx, &elasticloadbalancingv2.DescribeLoadBalancersInput{Marker: marker})
		if err != nil {
			return resources, err
		}
		for _, lb := range out.LoadBalancers {
			arn := awsString(lb.LoadBalancerArn)
			status := "active"
			if counts != nil {
				if counts[arn] == 0 {
					status = "idle"
				}
			}
			state := ""
			if lb.State != nil {
				state = string(lb.State.Code)
			}
			if state != "" && status == "active" {
				status = state
				if counts != nil && counts[arn] == 0 {
					status = "idle"
				}
			}
			resources = append(resources, Resource{
				ID:         arn,
				Name:       awsString(lb.LoadBalancerName),
				Provider:   "aws",
				Type:       ResourceELB,
				Region:     region,
				Status:     status,
				Spec:       string(lb.Type),
				CostUSD:    ELBBaseMonthly,
				CostSource: CostSourceEstimate,
				Tags:       map[string]string{},
			})
		}
		if awsString(out.NextMarker) == "" {
			break
		}
		marker = out.NextMarker
	}
	return resources, countErr
}

func targetCounts(ctx context.Context, api ELBAPI) (map[string]int, error) {
	counts := map[string]int{}
	var marker *string
	for {
		out, err := api.DescribeTargetGroups(ctx, &elasticloadbalancingv2.DescribeTargetGroupsInput{Marker: marker})
		if err != nil {
			return nil, err
		}
		for _, tg := range out.TargetGroups {
			health, err := api.DescribeTargetHealth(ctx, &elasticloadbalancingv2.DescribeTargetHealthInput{
				TargetGroupArn: tg.TargetGroupArn,
			})
			n := 0
			if err != nil {
				n = 1
			} else if health != nil {
				n = len(health.TargetHealthDescriptions)
			}
			for _, arn := range tg.LoadBalancerArns {
				counts[arn] += n
			}
		}
		if out == nil || awsString(out.NextMarker) == "" {
			break
		}
		marker = out.NextMarker
	}
	return counts, nil
}

func scanLambda(ctx context.Context, region string, api LambdaAPI) ([]Resource, error) {
	var marker *string
	var resources []Resource
	for {
		out, err := api.ListFunctions(ctx, &lambda.ListFunctionsInput{Marker: marker})
		if err != nil {
			return resources, err
		}
		for _, fn := range out.Functions {
			mem := aws.ToInt32(fn.MemorySize)
			resources = append(resources, Resource{
				ID:         firstNonEmpty(awsString(fn.FunctionArn), awsString(fn.FunctionName)),
				Name:       awsString(fn.FunctionName),
				Provider:   "aws",
				Type:       ResourceLambda,
				Region:     region,
				Status:     "active",
				Spec:       fmt.Sprintf("%s/%dMB", fn.Runtime, mem),
				CostUSD:    0,
				CostSource: CostSourceEstimate,
				Tags:       map[string]string{},
				Metadata:   map[string]string{"memory_mb": fmt.Sprintf("%d", mem)},
			})
		}
		if awsString(out.NextMarker) == "" {
			return resources, nil
		}
		marker = out.NextMarker
	}
}

func scanEKS(ctx context.Context, region string, api EKSAPI) ([]Resource, error) {
	var token *string
	var resources []Resource
	for {
		out, err := api.ListClusters(ctx, &eks.ListClustersInput{NextToken: token})
		if err != nil {
			return resources, err
		}
		for _, name := range out.Clusters {
			desc, err := api.DescribeCluster(ctx, &eks.DescribeClusterInput{Name: aws.String(name)})
			if err != nil {
				return resources, err
			}
			if desc.Cluster == nil {
				continue
			}
			resources = append(resources, Resource{
				ID:         awsString(desc.Cluster.Arn),
				Name:       awsString(desc.Cluster.Name),
				Provider:   "aws",
				Type:       ResourceEKS,
				Region:     region,
				Status:     string(desc.Cluster.Status),
				Spec:       awsString(desc.Cluster.Version),
				CostUSD:    EKSControlPlaneMonthly,
				CostSource: CostSourceEstimate,
				Tags:       desc.Cluster.Tags,
			})
		}
		if awsString(out.NextToken) == "" {
			return resources, nil
		}
		token = out.NextToken
	}
}

func scanASG(ctx context.Context, region string, api ASGAPI) ([]Resource, error) {
	var token *string
	var resources []Resource
	for {
		out, err := api.DescribeAutoScalingGroups(ctx, &autoscaling.DescribeAutoScalingGroupsInput{NextToken: token})
		if err != nil {
			return resources, err
		}
		for _, g := range out.AutoScalingGroups {
			desired := int(aws.ToInt32(g.DesiredCapacity))
			min := int(aws.ToInt32(g.MinSize))
			max := int(aws.ToInt32(g.MaxSize))
			resources = append(resources, Resource{
				ID:         awsString(g.AutoScalingGroupName),
				Name:       awsString(g.AutoScalingGroupName),
				Provider:   "aws",
				Type:       ResourceASG,
				Region:     region,
				Status:     "active",
				Spec:       fmt.Sprintf("desired=%d min=%d max=%d", desired, min, max),
				CostUSD:    0,
				CostSource: CostSourceEstimate,
				Tags:       asgTags(g.Tags),
				Metadata: map[string]string{
					"desired": fmt.Sprintf("%d", desired),
					"min":     fmt.Sprintf("%d", min),
					"max":     fmt.Sprintf("%d", max),
				},
			})
		}
		if awsString(out.NextToken) == "" {
			return resources, nil
		}
		token = out.NextToken
	}
}

func instanceName(tags []ec2types.Tag) string {
	for _, t := range tags {
		if awsString(t.Key) == "Name" {
			return awsString(t.Value)
		}
	}
	return ""
}

func tagsToMap(tags []ec2types.Tag) map[string]string {
	m := make(map[string]string, len(tags))
	for _, t := range tags {
		m[awsString(t.Key)] = awsString(t.Value)
	}
	return m
}

func rdsTags(tags []rdstypes.Tag) map[string]string {
	m := make(map[string]string, len(tags))
	for _, t := range tags {
		m[awsString(t.Key)] = awsString(t.Value)
	}
	return m
}

func s3Tags(tags []s3types.Tag) map[string]string {
	m := make(map[string]string, len(tags))
	for _, t := range tags {
		m[awsString(t.Key)] = awsString(t.Value)
	}
	return m
}

func asgTags(tags []astypes.TagDescription) map[string]string {
	m := make(map[string]string, len(tags))
	for _, t := range tags {
		m[awsString(t.Key)] = awsString(t.Value)
	}
	return m
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
