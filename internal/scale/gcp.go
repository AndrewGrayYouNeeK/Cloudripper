package scale

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/cloud"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/monitoring/v3"
	"google.golang.org/api/option"
)

// MIGAPI is the managed-instance-group surface the scaler needs.
type MIGAPI interface {
	List(ctx context.Context) ([]Group, error)
	CPU(ctx context.Context, g Group) (float64, error)
	Resize(ctx context.Context, g Group, desired int) error
	LookupInstance(ctx context.Context, name string) (Group, error)
	ResizeInstance(ctx context.Context, g Group, spec string) error
}

// GCPController scales managed instance groups and individual VMs.
type GCPController struct {
	API MIGAPI
}

// NewGCPController builds a controller from application default credentials.
func NewGCPController(ctx context.Context, project, region string) (*GCPController, error) {
	api, err := newGCPMIG(ctx, project, region)
	if err != nil {
		return nil, err
	}
	return &GCPController{API: api}, nil
}

// Provider implements Controller.
func (g *GCPController) Provider() string { return "gcp" }

// Discover lists managed instance groups.
func (g *GCPController) Discover(ctx context.Context) ([]Group, error) {
	return g.API.List(ctx)
}

// Lookup finds a MIG or a VM.
func (g *GCPController) Lookup(ctx context.Context, kind, name string) (Group, error) {
	switch kind {
	case KindMIG:
		groups, err := g.API.List(ctx)
		if err != nil {
			return Group{}, err
		}
		for _, group := range groups {
			if group.Name == name {
				return group, nil
			}
		}
		return Group{}, fmt.Errorf("managed instance group %s not found", name)
	case KindGCE:
		return g.API.LookupInstance(ctx, name)
	default:
		return Group{}, fmt.Errorf("gcp scale kind %q is unsupported", kind)
	}
}

// CPU reads average CPU. GCP utilization is a 0..1 ratio and is converted to percent.
func (g *GCPController) CPU(ctx context.Context, group Group) (float64, error) {
	return g.API.CPU(ctx, group)
}

// Apply resizes a group or a VM.
func (g *GCPController) Apply(ctx context.Context, group Group, decision Decision) error {
	switch decision.Action {
	case ActionNone:
		return nil
	case ActionScaleOut, ActionScaleIn:
		return g.API.Resize(ctx, group, decision.To)
	case ActionResizeUp, ActionResizeDown:
		var spec string
		var ok bool
		if decision.Action == ActionResizeUp {
			spec, ok = cloud.LargerSpec("gcp", group.Spec)
		} else {
			spec, ok = cloud.SmallerSpec("gcp", group.Spec)
		}
		if !ok {
			return fmt.Errorf("no adjacent size for %s", group.Spec)
		}
		return g.API.ResizeInstance(ctx, group, spec)
	default:
		return fmt.Errorf("unsupported scale action %s", decision.Action)
	}
}

// MIGCPUFilter selects instance CPU samples that belong to a managed instance group.
func MIGCPUFilter(name string) string {
	escaped := strings.ReplaceAll(name, `"`, "")
	return fmt.Sprintf(`metric.type="compute.googleapis.com/instance/cpu/utilization" AND resource.type="gce_instance" AND metadata.system_labels.instance_group="%s"`, escaped)
}

type gcpMIG struct {
	project string
	region  string
	compute *compute.Service
	mon     *monitoring.Service
	now     func() time.Time
}

func newGCPMIG(ctx context.Context, project, region string) (*gcpMIG, error) {
	if project == "" {
		return nil, fmt.Errorf("gcp project not configured")
	}
	computeSvc, err := compute.NewService(ctx, option.WithScopes(compute.CloudPlatformScope))
	if err != nil {
		return nil, err
	}
	mon, err := monitoring.NewService(ctx, option.WithScopes(monitoring.MonitoringReadScope))
	if err != nil {
		return nil, err
	}
	return &gcpMIG{project: project, region: region, compute: computeSvc, mon: mon}, nil
}

func (g *gcpMIG) List(ctx context.Context) ([]Group, error) {
	var groups []Group
	err := g.compute.InstanceGroupManagers.AggregatedList(g.project).Pages(ctx, func(page *compute.InstanceGroupManagerAggregatedList) error {
		for _, scoped := range page.Items {
			for _, mig := range scoped.InstanceGroupManagers {
				if mig == nil {
					continue
				}
				zone := cloud.LastPathSegment(mig.Zone)
				scope := "zone"
				loc := zone
				if zone == "" {
					scope = "region"
					loc = cloud.LastPathSegment(mig.Region)
				}
				if g.region != "" && loc != "" && !strings.HasPrefix(loc, g.region) && loc != g.region {
					continue
				}
				min := 0
				max := int(mig.TargetSize)
				if max < 1 {
					max = 1
				}
				groups = append(groups, Group{
					Provider: "gcp",
					Kind:     KindMIG,
					Name:     mig.Name,
					Region:   loc,
					Desired:  int(mig.TargetSize),
					Min:      min,
					Max:      max,
					Spec:     scope,
				})
			}
		}
		return nil
	})
	return groups, err
}

func (g *gcpMIG) CPU(ctx context.Context, group Group) (float64, error) {
	now := time.Now().UTC()
	if g.now != nil {
		now = g.now()
	}
	filter := MIGCPUFilter(group.Name)
	if group.Kind == KindGCE {
		filter = fmt.Sprintf(`metric.type="compute.googleapis.com/instance/cpu/utilization" AND resource.type="gce_instance" AND metric.labels.instance_name="%s"`, group.Name)
	}
	resp, err := g.mon.Projects.TimeSeries.List("projects/" + g.project).
		Filter(filter).
		IntervalStartTime(now.Add(-15 * time.Minute).Format(time.RFC3339)).
		IntervalEndTime(now.Format(time.RFC3339)).
		AggregationAlignmentPeriod("300s").
		AggregationPerSeriesAligner("ALIGN_MEAN").
		AggregationCrossSeriesReducer("REDUCE_MEAN").
		Context(ctx).Do()
	if err != nil {
		return 0, err
	}
	var samples []float64
	if resp != nil {
		for _, series := range resp.TimeSeries {
			for _, point := range series.Points {
				if point.Value != nil && point.Value.DoubleValue != nil {
					samples = append(samples, PercentFromFraction(*point.Value.DoubleValue))
				}
			}
		}
	}
	return Average(samples)
}

func (g *gcpMIG) Resize(ctx context.Context, group Group, desired int) error {
	if group.Spec == "region" {
		op, err := g.compute.RegionInstanceGroupManagers.Resize(g.project, group.Region, group.Name, int64(desired)).Context(ctx).Do()
		if err != nil {
			return err
		}
		return waitRegion(ctx, g.compute, g.project, group.Region, op)
	}
	op, err := g.compute.InstanceGroupManagers.Resize(g.project, group.Region, group.Name, int64(desired)).Context(ctx).Do()
	if err != nil {
		return err
	}
	return waitZone(ctx, g.compute, g.project, group.Region, op)
}

func (g *gcpMIG) LookupInstance(ctx context.Context, name string) (Group, error) {
	var found *compute.Instance
	var zone string
	err := g.compute.Instances.AggregatedList(g.project).Pages(ctx, func(page *compute.InstanceAggregatedList) error {
		for _, scoped := range page.Items {
			for _, inst := range scoped.Instances {
				if inst != nil && inst.Name == name {
					found = inst
					zone = cloud.LastPathSegment(inst.Zone)
					return nil
				}
			}
		}
		return nil
	})
	if err != nil {
		return Group{}, err
	}
	if found == nil {
		return Group{}, fmt.Errorf("instance %s not found", name)
	}
	return Group{
		Provider: "gcp",
		Kind:     KindGCE,
		Name:     found.Name,
		Region:   zone,
		Desired:  1,
		Min:      1,
		Max:      1,
		Spec:     cloud.LastPathSegment(found.MachineType),
	}, nil
}

func (g *gcpMIG) ResizeInstance(ctx context.Context, group Group, spec string) error {
	if _, err := g.compute.Instances.Stop(g.project, group.Region, group.Name).Context(ctx).Do(); err != nil {
		return err
	}
	if _, err := g.compute.Instances.SetMachineType(g.project, group.Region, group.Name, &compute.InstancesSetMachineTypeRequest{
		MachineType: fmt.Sprintf("zones/%s/machineTypes/%s", group.Region, spec),
	}).Context(ctx).Do(); err != nil {
		_, _ = g.compute.Instances.Start(g.project, group.Region, group.Name).Context(ctx).Do()
		return err
	}
	_, err := g.compute.Instances.Start(g.project, group.Region, group.Name).Context(ctx).Do()
	return err
}

func waitZone(ctx context.Context, svc *compute.Service, project, zone string, op *compute.Operation) error {
	return waitOp(ctx, op, func() (*compute.Operation, error) {
		return svc.ZoneOperations.Get(project, zone, op.Name).Context(ctx).Do()
	})
}

func waitRegion(ctx context.Context, svc *compute.Service, project, region string, op *compute.Operation) error {
	return waitOp(ctx, op, func() (*compute.Operation, error) {
		return svc.RegionOperations.Get(project, region, op.Name).Context(ctx).Do()
	})
}

func waitOp(ctx context.Context, op *compute.Operation, poll func() (*compute.Operation, error)) error {
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
