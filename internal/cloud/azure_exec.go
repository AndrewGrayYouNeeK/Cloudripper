package cloud

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/schedule"
)

func executeAzure(ctx context.Context, api AzureActions, r Resource, action, spec string) (string, error) {
	group := r.Meta("resource_group")
	name := r.Name
	if group == "" || name == "" {
		return "", fmt.Errorf("azure resource %s is missing name or resource group", r.ID)
	}
	switch r.Type {
	case ResourceAzureVM:
		return executeAzureVM(ctx, api, r, group, name, action, spec)
	case ResourceAzureDisk:
		if action != "terminate" {
			return "", &UnsupportedError{Action: action, Type: r.Type}
		}
		if !strings.EqualFold(r.Status, "unattached") {
			return "", fmt.Errorf("refusing to delete disk %s in state %s", name, r.Status)
		}
		if err := api.DeleteDisk(ctx, group, name); err != nil {
			return "", err
		}
		return "deleted disk " + name, nil
	case ResourceAzureIP:
		if action != "terminate" {
			return "", &UnsupportedError{Action: action, Type: r.Type}
		}
		if strings.EqualFold(r.Status, "in-use") {
			return "", fmt.Errorf("refusing to delete address %s while it is in use", name)
		}
		if err := api.DeleteAddress(ctx, group, name); err != nil {
			return "", err
		}
		return "deleted address " + name, nil
	case ResourceAzureLB:
		if action != "terminate" {
			return "", &UnsupportedError{Action: action, Type: r.Type}
		}
		if !strings.EqualFold(r.Status, "idle") {
			return "", fmt.Errorf("refusing to delete load balancer %s in state %s", name, r.Status)
		}
		if err := api.DeleteLoadBalancer(ctx, group, name); err != nil {
			return "", err
		}
		return "deleted load balancer " + name, nil
	default:
		return "", &UnsupportedError{Action: action, Type: r.Type, Reason: fmt.Sprintf("refusing to %s %s; this type is inventory-only", action, r.Type)}
	}
}

func executeAzureVM(ctx context.Context, api AzureActions, r Resource, group, name, action, spec string) (string, error) {
	switch action {
	case "stop":
		if err := api.DeallocateVM(ctx, group, name); err != nil {
			return "", err
		}
		return "deallocated " + name, nil
	case "start":
		if err := api.StartVM(ctx, group, name); err != nil {
			return "", err
		}
		return "started " + name, nil
	case "terminate":
		if err := api.DeleteVM(ctx, group, name); err != nil {
			return "", err
		}
		return "deleted " + name + " (irreversible; disks may remain)", nil
	case "rightsize":
		spec = CanonicalSpec(spec)
		if spec == "" {
			return "", fmt.Errorf("rightsize requires a target VM size")
		}
		wasRunning := strings.EqualFold(r.Status, "running")
		if wasRunning {
			if err := api.DeallocateVM(ctx, group, name); err != nil {
				return "", err
			}
		}
		if err := api.ResizeVM(ctx, group, name, spec); err != nil {
			if wasRunning {
				_ = api.StartVM(ctx, group, name)
			}
			return "", err
		}
		if wasRunning {
			if err := api.StartVM(ctx, group, name); err != nil {
				return "", fmt.Errorf("resized to %s but start failed: %w", spec, err)
			}
		}
		return "resized " + name + " -> " + spec, nil
	case "schedule":
		if spec == "" {
			spec = schedule.DefaultOffWindow
		}
		if _, err := schedule.InOffWindow(spec, time.Now().UTC()); err != nil {
			return "", err
		}
		if err := api.TagVM(ctx, group, name, schedule.Tag, spec); err != nil {
			return "", err
		}
		return "tagged " + schedule.Tag + "=" + spec, nil
	default:
		return "", &UnsupportedError{Action: action, Type: r.Type}
	}
}
