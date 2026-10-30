package cloud

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type locationEC2 struct {
	fakeEC2
	allRegions *bool
}

func (l *locationEC2) DescribeRegions(_ context.Context, params *ec2.DescribeRegionsInput, _ ...func(*ec2.Options)) (*ec2.DescribeRegionsOutput, error) {
	if params != nil {
		l.allRegions = params.AllRegions
	}
	return &ec2.DescribeRegionsOutput{Regions: []ec2types.Region{
		{RegionName: aws.String("us-east-1"), OptInStatus: aws.String("opt-in-not-required")},
		{RegionName: aws.String("us-east-2"), OptInStatus: aws.String("opt-in-not-required")},
		{RegionName: aws.String("af-south-1"), OptInStatus: aws.String("not-opted-in")},
		{RegionName: aws.String("eu-west-2"), OptInStatus: aws.String("opted-in")},
	}}, nil
}

type locationS3 struct {
	locations map[string]string
	locErr    map[string]error
	lists     int
}

func (s *locationS3) ListBuckets(context.Context, *s3.ListBucketsInput, ...func(*s3.Options)) (*s3.ListBucketsOutput, error) {
	s.lists++
	var buckets []s3types.Bucket
	for name := range s.locations {
		buckets = append(buckets, s3types.Bucket{Name: aws.String(name)})
	}
	for name := range s.locErr {
		if _, ok := s.locations[name]; ok {
			continue
		}
		buckets = append(buckets, s3types.Bucket{Name: aws.String(name)})
	}
	return &s3.ListBucketsOutput{Buckets: buckets}, nil
}

func (s *locationS3) GetBucketLocation(_ context.Context, params *s3.GetBucketLocationInput, _ ...func(*s3.Options)) (*s3.GetBucketLocationOutput, error) {
	name := aws.ToString(params.Bucket)
	if err := s.locErr[name]; err != nil {
		return nil, err
	}
	return &s3.GetBucketLocationOutput{LocationConstraint: s3types.BucketLocationConstraint(s.locations[name])}, nil
}

func (s *locationS3) GetBucketTagging(context.Context, *s3.GetBucketTaggingInput, ...func(*s3.Options)) (*s3.GetBucketTaggingOutput, error) {
	return nil, errors.New("no tags")
}

func TestScanS3FiltersByBucketLocation(t *testing.T) {
	api := &locationS3{locations: map[string]string{
		"east-logs": "",
		"ohio":      "us-east-2",
		"ireland":   "EU",
	}}
	got, err := scanS3(context.Background(), api, "us-east-2")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "ohio" || got[0].Region != "us-east-2" {
		t.Fatalf("got %+v", got)
	}

	all, err := scanS3(context.Background(), api, "")
	if err != nil {
		t.Fatal(err)
	}
	regions := map[string]string{}
	for _, bucket := range all {
		regions[bucket.Name] = bucket.Region
	}
	if regions["east-logs"] != "us-east-1" || regions["ireland"] != "eu-west-1" || regions["ohio"] != "us-east-2" {
		t.Fatalf("regions = %v", regions)
	}
}

func TestEnabledRegionsSkipsDisabledOptIn(t *testing.T) {
	api := &locationEC2{}
	got, err := EnabledRegions(context.Background(), api)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(got, ",")
	if joined != "eu-west-2,us-east-1,us-east-2" {
		t.Fatalf("regions = %s", joined)
	}
	if api.allRegions == nil || !*api.allRegions {
		t.Fatal("DescribeRegions should request every region so opt-in status is visible")
	}
}

func TestScanAllRegionsListsS3Once(t *testing.T) {
	s3api := &locationS3{locations: map[string]string{"ohio": "us-east-2", "east": ""}}
	ec2api := &locationEC2{}
	loads := 0
	provider := &AWSProvider{
		region:     "us-east-1",
		allRegions: true,
		load: func(context.Context, string) (AWSClients, error) {
			loads++
			return AWSClients{EC2: ec2api, S3: s3api}, nil
		},
	}
	got, err := provider.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s3api.lists != 1 {
		t.Fatalf("ListBuckets calls = %d, want 1", s3api.lists)
	}
	var buckets int
	for _, resource := range got {
		if resource.Type == ResourceS3 {
			buckets++
			if resource.Region == "" {
				t.Fatalf("blank region for %s", resource.Name)
			}
		}
	}
	if buckets != 2 {
		t.Fatalf("buckets = %d", buckets)
	}
	if loads < 2 {
		t.Fatalf("expected a client per enabled region, loads = %d", loads)
	}
}
