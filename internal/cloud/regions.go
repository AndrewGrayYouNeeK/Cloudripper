package cloud

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

// NormalizeBucketLocation maps GetBucketLocation values onto region codes.
// An empty constraint is us-east-1. The legacy EU constraint is eu-west-1.
func NormalizeBucketLocation(loc string) string {
	switch strings.ToLower(strings.TrimSpace(loc)) {
	case "", "null":
		return "us-east-1"
	case "eu":
		return "eu-west-1"
	default:
		return strings.TrimSpace(loc)
	}
}

// EnabledRegions returns regions the account can use.
// Opt-in regions that are not enabled are omitted. DescribeRegions itself
// failing is returned; a disabled opt-in region is not an error.
func EnabledRegions(ctx context.Context, api EC2API) ([]string, error) {
	if api == nil {
		return nil, fmt.Errorf("ec2 client not configured")
	}
	out, err := api.DescribeRegions(ctx, &ec2.DescribeRegionsInput{
		AllRegions: aws.Bool(true),
	})
	if err != nil {
		return nil, err
	}
	var regions []string
	for _, region := range out.Regions {
		name := strings.TrimSpace(aws.ToString(region.RegionName))
		if name == "" || !regionEnabled(aws.ToString(region.OptInStatus)) {
			continue
		}
		regions = append(regions, name)
	}
	sort.Strings(regions)
	if len(regions) == 0 {
		return nil, fmt.Errorf("no enabled regions returned")
	}
	return regions, nil
}

func regionEnabled(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "not-opted-in":
		return false
	default:
		return true
	}
}
