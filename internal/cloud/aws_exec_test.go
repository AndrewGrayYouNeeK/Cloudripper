package cloud

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

type fakeEC2 struct {
	state     string
	typ       string
	calls     []string
	modifyErr error
	released  string
	deleted   string
	regions   []ec2types.Region
}

func (f *fakeEC2) DescribeInstances(context.Context, *ec2.DescribeInstancesInput, ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	f.calls = append(f.calls, "describe")
	return &ec2.DescribeInstancesOutput{
		Reservations: []ec2types.Reservation{{
			Instances: []ec2types.Instance{{
				InstanceType: ec2types.InstanceType(f.typ),
				State:        &ec2types.InstanceState{Name: ec2types.InstanceStateName(f.state)},
			}},
		}},
	}, nil
}

func (f *fakeEC2) DescribeVolumes(context.Context, *ec2.DescribeVolumesInput, ...func(*ec2.Options)) (*ec2.DescribeVolumesOutput, error) {
	return &ec2.DescribeVolumesOutput{}, nil
}

func (f *fakeEC2) DescribeAddresses(context.Context, *ec2.DescribeAddressesInput, ...func(*ec2.Options)) (*ec2.DescribeAddressesOutput, error) {
	return &ec2.DescribeAddressesOutput{}, nil
}

func (f *fakeEC2) StopInstances(context.Context, *ec2.StopInstancesInput, ...func(*ec2.Options)) (*ec2.StopInstancesOutput, error) {
	f.calls = append(f.calls, "stop")
	f.state = "stopped"
	return &ec2.StopInstancesOutput{}, nil
}

func (f *fakeEC2) StartInstances(context.Context, *ec2.StartInstancesInput, ...func(*ec2.Options)) (*ec2.StartInstancesOutput, error) {
	f.calls = append(f.calls, "start")
	f.state = "running"
	return &ec2.StartInstancesOutput{}, nil
}

func (f *fakeEC2) TerminateInstances(context.Context, *ec2.TerminateInstancesInput, ...func(*ec2.Options)) (*ec2.TerminateInstancesOutput, error) {
	f.calls = append(f.calls, "terminate")
	return &ec2.TerminateInstancesOutput{}, nil
}

func (f *fakeEC2) ModifyInstanceAttribute(_ context.Context, params *ec2.ModifyInstanceAttributeInput, _ ...func(*ec2.Options)) (*ec2.ModifyInstanceAttributeOutput, error) {
	f.calls = append(f.calls, "modify")
	if f.modifyErr != nil {
		return nil, f.modifyErr
	}
	if params.InstanceType != nil {
		f.typ = aws.ToString(params.InstanceType.Value)
	}
	return &ec2.ModifyInstanceAttributeOutput{}, nil
}

func (f *fakeEC2) CreateTags(context.Context, *ec2.CreateTagsInput, ...func(*ec2.Options)) (*ec2.CreateTagsOutput, error) {
	f.calls = append(f.calls, "tag")
	return &ec2.CreateTagsOutput{}, nil
}

func (f *fakeEC2) DeleteVolume(_ context.Context, params *ec2.DeleteVolumeInput, _ ...func(*ec2.Options)) (*ec2.DeleteVolumeOutput, error) {
	f.deleted = aws.ToString(params.VolumeId)
	return &ec2.DeleteVolumeOutput{}, nil
}

func (f *fakeEC2) ReleaseAddress(_ context.Context, params *ec2.ReleaseAddressInput, _ ...func(*ec2.Options)) (*ec2.ReleaseAddressOutput, error) {
	f.released = aws.ToString(params.AllocationId)
	return &ec2.ReleaseAddressOutput{}, nil
}

func (f *fakeEC2) DescribeRegions(context.Context, *ec2.DescribeRegionsInput, ...func(*ec2.Options)) (*ec2.DescribeRegionsOutput, error) {
	return &ec2.DescribeRegionsOutput{Regions: f.regions}, nil
}

func joined(calls []string) string { return strings.Join(calls, ",") }

func TestResizeEC2RunningRestarts(t *testing.T) {
	api := &fakeEC2{state: "running", typ: "t3.small"}
	detail, err := ResizeEC2(context.Background(), api, "i-1", "t3.micro")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(joined(api.calls), "stop,describe,modify,start") {
		t.Fatalf("calls = %s", joined(api.calls))
	}
	if api.typ != "t3.micro" || !strings.Contains(detail, "t3.micro") {
		t.Fatalf("detail %q type %s", detail, api.typ)
	}
}

func TestResizeEC2LeavesStoppedInstanceStopped(t *testing.T) {
	api := &fakeEC2{state: "stopped", typ: "t3.small"}
	if _, err := ResizeEC2(context.Background(), api, "i-1", "t3.micro"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(joined(api.calls), "stop") || strings.Contains(joined(api.calls), "start") {
		t.Fatalf("stopped resize should only modify, calls = %s", joined(api.calls))
	}
}

func TestResizeEC2RestartsWhenModifyFails(t *testing.T) {
	api := &fakeEC2{state: "running", typ: "t3.small", modifyErr: errors.New("denied")}
	_, err := ResizeEC2(context.Background(), api, "i-1", "t3.micro")
	if err == nil || !strings.Contains(err.Error(), "restarted") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(joined(api.calls), "start") {
		t.Fatalf("calls = %s", joined(api.calls))
	}
}

func TestResizeEC2StoppedModifyFailureStaysStopped(t *testing.T) {
	api := &fakeEC2{state: "stopped", typ: "t3.small", modifyErr: errors.New("denied")}
	_, err := ResizeEC2(context.Background(), api, "i-1", "t3.micro")
	if err == nil || !strings.Contains(err.Error(), "left stopped") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(joined(api.calls), "start") {
		t.Fatalf("calls = %s", joined(api.calls))
	}
}

func TestExecuteVolumeAndAddressGuards(t *testing.T) {
	api := &fakeEC2{}
	_, err := executeEBS(context.Background(), api, Resource{ID: "vol-1", Type: ResourceEBS, Status: "in-use"}, "terminate")
	if err == nil || api.deleted != "" {
		t.Fatalf("in-use volume deleted (%q), err %v", api.deleted, err)
	}
	if _, err := executeEBS(context.Background(), api, Resource{ID: "vol-2", Type: ResourceEBS, Status: "available"}, "terminate"); err != nil {
		t.Fatal(err)
	}
	if api.deleted != "vol-2" {
		t.Fatalf("deleted %q", api.deleted)
	}
	_, err = executeEIP(context.Background(), api, Resource{ID: "eipalloc-1", Type: ResourceEIP, Status: "in-use"}, "terminate")
	if err == nil || api.released != "" {
		t.Fatalf("in-use address released (%q), err %v", api.released, err)
	}
	if _, err := executeEIP(context.Background(), api, Resource{ID: "eipalloc-2", Type: ResourceEIP, Status: "unattached"}, "terminate"); err != nil {
		t.Fatal(err)
	}
	if api.released != "eipalloc-2" {
		t.Fatalf("released %q", api.released)
	}
}
