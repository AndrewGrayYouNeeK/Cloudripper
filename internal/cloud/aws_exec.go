package cloud

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/schedule"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/rds"
)

func executeAWS(ctx context.Context, c AWSClients, r Resource, action, spec string) (string, error) {
	switch r.Type {
	case ResourceEC2:
		return executeEC2(ctx, c.EC2, r, action, spec)
	case ResourceRDS:
		return executeRDS(ctx, c.RDS, r, action, spec)
	case ResourceEBS:
		return executeEBS(ctx, c.EC2, r, action)
	case ResourceEIP:
		return executeEIP(ctx, c.EC2, r, action)
	case ResourceELB:
		return executeELB(ctx, c.ELB, r, action)
	default:
		return "", &UnsupportedError{Action: action, Type: r.Type, Reason: fmt.Sprintf("refusing to %s %s; this type is inventory-only", action, r.Type)}
	}
}

func executeEC2(ctx context.Context, api EC2API, r Resource, action, spec string) (string, error) {
	if api == nil {
		return "", fmt.Errorf("ec2 client not configured")
	}
	switch action {
	case "stop":
		if strings.EqualFold(r.Status, "stopped") {
			return "already stopped", nil
		}
		_, err := api.StopInstances(ctx, &ec2.StopInstancesInput{InstanceIds: []string{r.ID}})
		if err != nil {
			return "", err
		}
		if err := waitInstance(ctx, api, r.ID, "stopped"); err != nil {
			return "", err
		}
		return "stopped " + r.ID, nil
	case "start":
		if strings.EqualFold(r.Status, "running") {
			return "already running", nil
		}
		_, err := api.StartInstances(ctx, &ec2.StartInstancesInput{InstanceIds: []string{r.ID}})
		if err != nil {
			return "", err
		}
		if err := waitInstance(ctx, api, r.ID, "running"); err != nil {
			return "", err
		}
		return "started " + r.ID, nil
	case "terminate":
		_, err := api.TerminateInstances(ctx, &ec2.TerminateInstancesInput{InstanceIds: []string{r.ID}})
		if err != nil {
			return "", err
		}
		return "terminated " + r.ID + " (irreversible)", nil
	case "rightsize":
		return resizeEC2(ctx, api, r.ID, spec)
	case "schedule":
		if spec == "" {
			spec = schedule.DefaultOffWindow
		}
		if _, err := schedule.InOffWindow(spec, time.Now().UTC()); err != nil {
			return "", err
		}
		_, err := api.CreateTags(ctx, &ec2.CreateTagsInput{
			Resources: []string{r.ID},
			Tags: []ec2types.Tag{{
				Key:   aws.String(schedule.Tag),
				Value: aws.String(spec),
			}},
		})
		if err != nil {
			return "", err
		}
		return "tagged " + schedule.Tag + "=" + spec, nil
	default:
		return "", &UnsupportedError{Action: action, Type: r.Type}
	}
}

// ResizeEC2 stops a running instance when required, changes its type, and
// starts it again only if it was running before the resize. A failed modify
// attempts to restart an instance that was running.
func ResizeEC2(ctx context.Context, api EC2API, id, newType string) (string, error) {
	return resizeEC2(ctx, api, id, newType)
}

func resizeEC2(ctx context.Context, api EC2API, id, newType string) (string, error) {
	if api == nil {
		return "", fmt.Errorf("ec2 client not configured")
	}
	newType = CanonicalSpec(newType)
	if newType == "" {
		return "", fmt.Errorf("rightsize requires a target instance type")
	}
	state, current, err := describeInstance(ctx, api, id)
	if err != nil {
		return "", err
	}
	if strings.EqualFold(current, newType) {
		return "already " + newType, nil
	}
	wasRunning := strings.EqualFold(state, "running")
	if !strings.EqualFold(state, "stopped") {
		if _, err := api.StopInstances(ctx, &ec2.StopInstancesInput{InstanceIds: []string{id}}); err != nil {
			return "", err
		}
		if err := waitInstance(ctx, api, id, "stopped"); err != nil {
			return "", err
		}
	}
	_, err = api.ModifyInstanceAttribute(ctx, &ec2.ModifyInstanceAttributeInput{
		InstanceId: aws.String(id),
		InstanceType: &ec2types.AttributeValue{
			Value: aws.String(newType),
		},
	})
	if err != nil {
		if wasRunning {
			_, startErr := api.StartInstances(ctx, &ec2.StartInstancesInput{InstanceIds: []string{id}})
			if startErr != nil {
				return "", fmt.Errorf("resize failed (%v) and restart failed (%v)", err, startErr)
			}
			return "", fmt.Errorf("resize failed (%v); instance was restarted", err)
		}
		return "", fmt.Errorf("resize failed (%v); instance left stopped", err)
	}
	if wasRunning {
		if _, err := api.StartInstances(ctx, &ec2.StartInstancesInput{InstanceIds: []string{id}}); err != nil {
			return "", fmt.Errorf("resized to %s but restart failed: %w", newType, err)
		}
		if err := waitInstance(ctx, api, id, "running"); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("resized %s -> %s", current, newType), nil
}

func executeRDS(ctx context.Context, api RDSAPI, r Resource, action, spec string) (string, error) {
	if api == nil {
		return "", fmt.Errorf("rds client not configured")
	}
	switch action {
	case "stop":
		_, err := api.StopDBInstance(ctx, &rds.StopDBInstanceInput{DBInstanceIdentifier: aws.String(r.ID)})
		if err != nil {
			return "", err
		}
		return "stopping database " + r.ID, nil
	case "start":
		_, err := api.StartDBInstance(ctx, &rds.StartDBInstanceInput{DBInstanceIdentifier: aws.String(r.ID)})
		if err != nil {
			return "", err
		}
		return "starting database " + r.ID, nil
	case "rightsize":
		spec = CanonicalSpec(spec)
		if spec == "" {
			return "", fmt.Errorf("rightsize requires a target instance class")
		}
		_, err := api.ModifyDBInstance(ctx, &rds.ModifyDBInstanceInput{
			DBInstanceIdentifier: aws.String(r.ID),
			DBInstanceClass:      aws.String(spec),
			ApplyImmediately:     aws.Bool(true),
		})
		if err != nil {
			return "", err
		}
		return "modifying " + r.ID + " to " + spec + " (database deletion is not supported)", nil
	case "terminate":
		return "", &UnsupportedError{Action: action, Type: r.Type, Reason: "refusing to delete databases; stop or rightsize them instead"}
	default:
		return "", &UnsupportedError{Action: action, Type: r.Type}
	}
}

func executeEBS(ctx context.Context, api EC2API, r Resource, action string) (string, error) {
	if action != "terminate" {
		return "", &UnsupportedError{Action: action, Type: r.Type}
	}
	if !strings.EqualFold(r.Status, "available") && !strings.EqualFold(r.Status, "unattached") {
		return "", fmt.Errorf("refusing to delete volume %s in state %s", r.ID, r.Status)
	}
	if api == nil {
		return "", fmt.Errorf("ec2 client not configured")
	}
	_, err := api.DeleteVolume(ctx, &ec2.DeleteVolumeInput{VolumeId: aws.String(r.ID)})
	if err != nil {
		return "", err
	}
	return "deleted volume " + r.ID, nil
}

func executeEIP(ctx context.Context, api EC2API, r Resource, action string) (string, error) {
	if action != "terminate" {
		return "", &UnsupportedError{Action: action, Type: r.Type}
	}
	if strings.EqualFold(r.Status, "in-use") {
		return "", fmt.Errorf("refusing to release address %s while it is associated", r.ID)
	}
	if api == nil {
		return "", fmt.Errorf("ec2 client not configured")
	}
	_, err := api.ReleaseAddress(ctx, &ec2.ReleaseAddressInput{AllocationId: aws.String(r.ID)})
	if err != nil {
		return "", err
	}
	return "released address " + r.ID, nil
}

func executeELB(ctx context.Context, api ELBAPI, r Resource, action string) (string, error) {
	if action != "terminate" {
		return "", &UnsupportedError{Action: action, Type: r.Type}
	}
	if !strings.EqualFold(r.Status, "idle") {
		return "", fmt.Errorf("refusing to delete load balancer %s in state %s", r.ID, r.Status)
	}
	if api == nil {
		return "", fmt.Errorf("elb client not configured")
	}
	_, err := api.DeleteLoadBalancer(ctx, &elasticloadbalancingv2.DeleteLoadBalancerInput{
		LoadBalancerArn: aws.String(r.ID),
	})
	if err != nil {
		return "", err
	}
	return "deleted load balancer " + r.Name, nil
}

func describeInstance(ctx context.Context, api EC2API, id string) (state, instanceType string, err error) {
	out, err := api.DescribeInstances(ctx, &ec2.DescribeInstancesInput{InstanceIds: []string{id}})
	if err != nil {
		return "", "", err
	}
	for _, res := range out.Reservations {
		for _, inst := range res.Instances {
			if inst.State != nil {
				state = string(inst.State.Name)
			}
			return state, string(inst.InstanceType), nil
		}
	}
	return "", "", fmt.Errorf("instance %s not found", id)
}

func waitInstance(ctx context.Context, api EC2API, id, want string) error {
	deadline := time.Now().Add(5 * time.Minute)
	for {
		state, _, err := describeInstance(ctx, api, id)
		if err != nil {
			return err
		}
		if strings.EqualFold(state, want) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("instance %s stayed %s, wanted %s", id, state, want)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}
