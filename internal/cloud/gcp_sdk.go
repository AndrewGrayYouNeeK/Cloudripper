package cloud

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/api/cloudfunctions/v1"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/container/v1"
	"google.golang.org/api/option"
	"google.golang.org/api/storage/v1"
)

type gcpSDK struct {
	project string
	compute *compute.Service
	storage *storage.Service
	fn      *cloudfunctions.Service
	gke     *container.Service
}

func connectGCP(ctx context.Context, project, _ string) (GCPLister, GCPActions, error) {
	if project == "" {
		return nil, nil, fmt.Errorf("gcp project not configured (set GCP_PROJECT)")
	}
	computeSvc, err := compute.NewService(ctx, option.WithScopes(compute.CloudPlatformScope))
	if err != nil {
		return nil, nil, fmt.Errorf("gcp compute client: %w", err)
	}
	sdk := &gcpSDK{project: project, compute: computeSvc}
	if storageSvc, err := storage.NewService(ctx, option.WithScopes(storage.CloudPlatformScope)); err == nil {
		sdk.storage = storageSvc
	}
	if fnSvc, err := cloudfunctions.NewService(ctx, option.WithScopes(cloudfunctions.CloudPlatformScope)); err == nil {
		sdk.fn = fnSvc
	}
	if gkeSvc, err := container.NewService(ctx, option.WithScopes(container.CloudPlatformScope)); err == nil {
		sdk.gke = gkeSvc
	}
	return sdk, sdk, nil
}

func (g *gcpSDK) Inventory(ctx context.Context) (GCPInventory, error) {
	var inv GCPInventory
	var errs []error
	if err := g.compute.Instances.AggregatedList(g.project).Pages(ctx, func(page *compute.InstanceAggregatedList) error {
		for _, scoped := range page.Items {
			inv.Instances = append(inv.Instances, scoped.Instances...)
		}
		return nil
	}); err != nil {
		errs = append(errs, fmt.Errorf("instances: %w", err))
	}
	if err := g.compute.Disks.AggregatedList(g.project).Pages(ctx, func(page *compute.DiskAggregatedList) error {
		for _, scoped := range page.Items {
			inv.Disks = append(inv.Disks, scoped.Disks...)
		}
		return nil
	}); err != nil {
		errs = append(errs, fmt.Errorf("disks: %w", err))
	}
	if err := g.compute.Addresses.AggregatedList(g.project).Pages(ctx, func(page *compute.AddressAggregatedList) error {
		for _, scoped := range page.Items {
			inv.Addresses = append(inv.Addresses, scoped.Addresses...)
		}
		return nil
	}); err != nil {
		errs = append(errs, fmt.Errorf("addresses: %w", err))
	}
	if err := g.compute.ForwardingRules.AggregatedList(g.project).Pages(ctx, func(page *compute.ForwardingRuleAggregatedList) error {
		for _, scoped := range page.Items {
			inv.ForwardingRules = append(inv.ForwardingRules, scoped.ForwardingRules...)
		}
		return nil
	}); err != nil {
		errs = append(errs, fmt.Errorf("forwarding rules: %w", err))
	}
	if err := g.compute.GlobalForwardingRules.List(g.project).Pages(ctx, func(page *compute.ForwardingRuleList) error {
		inv.ForwardingRules = append(inv.ForwardingRules, page.Items...)
		return nil
	}); err != nil {
		errs = append(errs, fmt.Errorf("global forwarding rules: %w", err))
	}
	if err := g.compute.InstanceGroupManagers.AggregatedList(g.project).Pages(ctx, func(page *compute.InstanceGroupManagerAggregatedList) error {
		for _, scoped := range page.Items {
			inv.MIGs = append(inv.MIGs, scoped.InstanceGroupManagers...)
		}
		return nil
	}); err != nil {
		errs = append(errs, fmt.Errorf("instance groups: %w", err))
	}
	if g.storage != nil {
		if err := g.storage.Buckets.List(g.project).Pages(ctx, func(page *storage.Buckets) error {
			inv.Buckets = append(inv.Buckets, page.Items...)
			return nil
		}); err != nil {
			errs = append(errs, fmt.Errorf("gcs: %w", err))
		}
	}
	if g.fn != nil {
		if err := g.fn.Projects.Locations.Functions.List("projects/"+g.project+"/locations/-").Pages(ctx, func(page *cloudfunctions.ListFunctionsResponse) error {
			inv.Functions = append(inv.Functions, page.Functions...)
			return nil
		}); err != nil {
			errs = append(errs, fmt.Errorf("functions: %w", err))
		}
	}
	if g.gke != nil {
		resp, err := g.gke.Projects.Locations.Clusters.List("projects/" + g.project + "/locations/-").Context(ctx).Do()
		if err != nil {
			errs = append(errs, fmt.Errorf("gke: %w", err))
		} else if resp != nil {
			inv.Clusters = append(inv.Clusters, resp.Clusters...)
		}
	}
	if len(inv.Instances) == 0 && len(inv.Disks) == 0 && len(inv.Buckets) == 0 && len(errs) > 0 {
		return inv, errs[0]
	}
	if len(errs) > 0 {
		return inv, fmt.Errorf("partial gcp scan: %v", errs[0])
	}
	return inv, nil
}

func (g *gcpSDK) StopInstance(ctx context.Context, zone, name string) error {
	op, err := g.compute.Instances.Stop(g.project, zone, name).Context(ctx).Do()
	if err != nil {
		return err
	}
	return g.waitZone(ctx, zone, op)
}

func (g *gcpSDK) StartInstance(ctx context.Context, zone, name string) error {
	op, err := g.compute.Instances.Start(g.project, zone, name).Context(ctx).Do()
	if err != nil {
		return err
	}
	return g.waitZone(ctx, zone, op)
}

func (g *gcpSDK) DeleteInstance(ctx context.Context, zone, name string) error {
	op, err := g.compute.Instances.Delete(g.project, zone, name).Context(ctx).Do()
	if err != nil {
		return err
	}
	return g.waitZone(ctx, zone, op)
}

func (g *gcpSDK) SetMachineType(ctx context.Context, zone, name, machineType string) error {
	op, err := g.compute.Instances.SetMachineType(g.project, zone, name, &compute.InstancesSetMachineTypeRequest{
		MachineType: fmt.Sprintf("zones/%s/machineTypes/%s", zone, machineType),
	}).Context(ctx).Do()
	if err != nil {
		return err
	}
	return g.waitZone(ctx, zone, op)
}

func (g *gcpSDK) SetLabel(ctx context.Context, zone, name, key, value string) error {
	inst, err := g.compute.Instances.Get(g.project, zone, name).Context(ctx).Do()
	if err != nil {
		return err
	}
	labels := map[string]string{}
	for k, v := range inst.Labels {
		labels[k] = v
	}
	labels[key] = value
	op, err := g.compute.Instances.SetLabels(g.project, zone, name, &compute.InstancesSetLabelsRequest{
		LabelFingerprint: inst.LabelFingerprint,
		Labels:           labels,
	}).Context(ctx).Do()
	if err != nil {
		return err
	}
	return g.waitZone(ctx, zone, op)
}

func (g *gcpSDK) DeleteDisk(ctx context.Context, zone, name string) error {
	op, err := g.compute.Disks.Delete(g.project, zone, name).Context(ctx).Do()
	if err != nil {
		return err
	}
	return g.waitZone(ctx, zone, op)
}

func (g *gcpSDK) DeleteAddress(ctx context.Context, scope, region, name string) error {
	if scope == "global" {
		op, err := g.compute.GlobalAddresses.Delete(g.project, name).Context(ctx).Do()
		if err != nil {
			return err
		}
		return g.waitGlobal(ctx, op)
	}
	op, err := g.compute.Addresses.Delete(g.project, region, name).Context(ctx).Do()
	if err != nil {
		return err
	}
	return g.waitRegion(ctx, region, op)
}

func (g *gcpSDK) DeleteForwardingRule(ctx context.Context, scope, region, name string) error {
	if scope == "global" {
		op, err := g.compute.GlobalForwardingRules.Delete(g.project, name).Context(ctx).Do()
		if err != nil {
			return err
		}
		return g.waitGlobal(ctx, op)
	}
	op, err := g.compute.ForwardingRules.Delete(g.project, region, name).Context(ctx).Do()
	if err != nil {
		return err
	}
	return g.waitRegion(ctx, region, op)
}

func (g *gcpSDK) ResizeMIG(ctx context.Context, scope, location, name string, size int64) error {
	if scope == "region" {
		op, err := g.compute.RegionInstanceGroupManagers.Resize(g.project, location, name, size).Context(ctx).Do()
		if err != nil {
			return err
		}
		return g.waitRegion(ctx, location, op)
	}
	op, err := g.compute.InstanceGroupManagers.Resize(g.project, location, name, size).Context(ctx).Do()
	if err != nil {
		return err
	}
	return g.waitZone(ctx, location, op)
}

func (g *gcpSDK) waitZone(ctx context.Context, zone string, op *compute.Operation) error {
	return g.wait(ctx, op, func() (*compute.Operation, error) {
		return g.compute.ZoneOperations.Get(g.project, zone, op.Name).Context(ctx).Do()
	})
}

func (g *gcpSDK) waitRegion(ctx context.Context, region string, op *compute.Operation) error {
	return g.wait(ctx, op, func() (*compute.Operation, error) {
		return g.compute.RegionOperations.Get(g.project, region, op.Name).Context(ctx).Do()
	})
}

func (g *gcpSDK) waitGlobal(ctx context.Context, op *compute.Operation) error {
	return g.wait(ctx, op, func() (*compute.Operation, error) {
		return g.compute.GlobalOperations.Get(g.project, op.Name).Context(ctx).Do()
	})
}

func (g *gcpSDK) wait(ctx context.Context, op *compute.Operation, poll func() (*compute.Operation, error)) error {
	if op == nil {
		return nil
	}
	deadline := time.Now().Add(5 * time.Minute)
	for {
		if op.Status == "DONE" {
			if op.Error != nil && len(op.Error.Errors) > 0 {
				return fmt.Errorf("gcp operation: %s", op.Error.Errors[0].Message)
			}
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("gcp operation %s timed out", op.Name)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
		next, err := poll()
		if err != nil {
			return err
		}
		op = next
	}
}
