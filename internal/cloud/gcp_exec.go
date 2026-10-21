package cloud

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/AndrewGrayYouNeeK/cloudripper/internal/schedule"
)

func executeGCP(ctx context.Context, api GCPActions, r Resource, action, spec string) (string, error) {
	switch r.Type {
	case ResourceGCE:
		return executeGCE(ctx, api, r, action, spec)
	case ResourceGCPDisk:
		return executeGCPDisk(ctx, api, r, action)
	case ResourceGCPIP:
		return executeGCPIP(ctx, api, r, action)
	case ResourceGCPLB:
		return executeGCPLB(ctx, api, r, action)
	case ResourceMIG:
		if action != "rightsize" && action != "scale" {
			return "", &UnsupportedError{Action: action, Type: r.Type, Reason: "resize managed instance groups with the scale command"}
		}
		return "", &UnsupportedError{Action: action, Type: r.Type, Reason: "resize managed instance groups with the scale command"}
	default:
		return "", &UnsupportedError{Action: action, Type: r.Type, Reason: fmt.Sprintf("refusing to %s %s; this type is inventory-only", action, r.Type)}
	}
}

func executeGCE(ctx context.Context, api GCPActions, r Resource, action, spec string) (string, error) {
	zone := firstNonEmpty(r.Meta("zone"), r.Region)
	name := firstNonEmpty(r.Name, r.ID)
	switch action {
	case "stop":
		if err := api.StopInstance(ctx, zone, name); err != nil {
			return "", err
		}
		return "stopped " + name, nil
	case "start":
		if err := api.StartInstance(ctx, zone, name); err != nil {
			return "", err
		}
		return "started " + name, nil
	case "terminate":
		if err := api.DeleteInstance(ctx, zone, name); err != nil {
			return "", err
		}
		return "deleted " + name + " (irreversible)", nil
	case "rightsize":
		spec = CanonicalSpec(spec)
		if spec == "" {
			return "", fmt.Errorf("rightsize requires a target machine type")
		}
		wasRunning := strings.EqualFold(r.Status, "RUNNING") || strings.EqualFold(r.Status, "running")
		if !strings.EqualFold(r.Status, "TERMINATED") && !strings.EqualFold(r.Status, "STOPPED") {
			if err := api.StopInstance(ctx, zone, name); err != nil {
				return "", err
			}
		}
		if err := api.SetMachineType(ctx, zone, name, spec); err != nil {
			if wasRunning {
				_ = api.StartInstance(ctx, zone, name)
			}
			return "", err
		}
		if wasRunning {
			if err := api.StartInstance(ctx, zone, name); err != nil {
				return "", fmt.Errorf("resized to %s but restart failed: %w", spec, err)
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
		if err := api.SetLabel(ctx, zone, name, schedule.Tag, spec); err != nil {
			return "", err
		}
		return "labeled " + schedule.Tag + "=" + spec, nil
	default:
		return "", &UnsupportedError{Action: action, Type: r.Type}
	}
}

func executeGCPDisk(ctx context.Context, api GCPActions, r Resource, action string) (string, error) {
	if action != "terminate" {
		return "", &UnsupportedError{Action: action, Type: r.Type}
	}
	if !strings.EqualFold(r.Status, "unattached") {
		return "", fmt.Errorf("refusing to delete disk %s in state %s", r.Name, r.Status)
	}
	zone := firstNonEmpty(r.Meta("zone"), r.Region)
	if err := api.DeleteDisk(ctx, zone, r.Name); err != nil {
		return "", err
	}
	return "deleted disk " + r.Name, nil
}

func executeGCPIP(ctx context.Context, api GCPActions, r Resource, action string) (string, error) {
	if action != "terminate" {
		return "", &UnsupportedError{Action: action, Type: r.Type}
	}
	if strings.EqualFold(r.Status, "in-use") {
		return "", fmt.Errorf("refusing to release address %s while it is in use", r.Name)
	}
	if err := api.DeleteAddress(ctx, r.Meta("scope"), r.Meta("region"), r.Name); err != nil {
		return "", err
	}
	return "released address " + r.Name, nil
}

func executeGCPLB(ctx context.Context, api GCPActions, r Resource, action string) (string, error) {
	if action != "terminate" {
		return "", &UnsupportedError{Action: action, Type: r.Type}
	}
	if !strings.EqualFold(r.Status, "idle") {
		return "", fmt.Errorf("refusing to delete forwarding rule %s in state %s", r.Name, r.Status)
	}
	if err := api.DeleteForwardingRule(ctx, r.Meta("scope"), r.Meta("region"), r.Name); err != nil {
		return "", err
	}
	return "deleted forwarding rule " + r.Name, nil
}
