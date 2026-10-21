package cloud

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"google.golang.org/api/cloudfunctions/v1"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/container/v1"
	"google.golang.org/api/storage/v1"
)

// GCPInventory is a cloud-agnostic snapshot of the GCP resources we understand.
type GCPInventory struct {
	Instances       []*compute.Instance
	Disks           []*compute.Disk
	Addresses       []*compute.Address
	ForwardingRules []*compute.ForwardingRule
	Buckets         []*storage.Bucket
	Functions       []*cloudfunctions.CloudFunction
	Clusters        []*container.Cluster
	MIGs            []*compute.InstanceGroupManager
}

// GCPLister lists a project. Tests fake this; the SDK adapter implements it.
type GCPLister interface {
	Inventory(ctx context.Context) (GCPInventory, error)
}

// GCPActions mutates GCP resources.
type GCPActions interface {
	StopInstance(ctx context.Context, zone, name string) error
	StartInstance(ctx context.Context, zone, name string) error
	DeleteInstance(ctx context.Context, zone, name string) error
	SetMachineType(ctx context.Context, zone, name, machineType string) error
	SetLabel(ctx context.Context, zone, name, key, value string) error
	DeleteDisk(ctx context.Context, zone, name string) error
	DeleteAddress(ctx context.Context, scope, region, name string) error
	DeleteForwardingRule(ctx context.Context, scope, region, name string) error
	ResizeMIG(ctx context.Context, scope, location, name string, size int64) error
}

// GCPProvider scans and mutates one Google Cloud project.
type GCPProvider struct {
	project string
	region  string
	lister  GCPLister
	actions GCPActions
	connect func(ctx context.Context) (GCPLister, GCPActions, error)

	mu      sync.Mutex
	loaded  bool
	loadErr error
}

// NewGCPProvider creates a provider that authenticates with application default credentials.
func NewGCPProvider(project, region string) *GCPProvider {
	p := &GCPProvider{project: project, region: region}
	p.connect = func(ctx context.Context) (GCPLister, GCPActions, error) {
		return connectGCP(ctx, project, region)
	}
	return p
}

// NewGCPProviderWith wires a fake lister and action set.
func NewGCPProviderWith(project, region string, lister GCPLister, actions GCPActions) *GCPProvider {
	return &GCPProvider{project: project, region: region, lister: lister, actions: actions, loaded: true}
}

// Name implements Provider.
func (g *GCPProvider) Name() string { return "gcp" }

// Provider implements Executor.
func (g *GCPProvider) Provider() string { return "gcp" }

// Scan implements Provider.
func (g *GCPProvider) Scan(ctx context.Context) ([]Resource, error) {
	if err := g.ensure(ctx); err != nil {
		return nil, err
	}
	inv, err := g.lister.Inventory(ctx)
	resources := ResourcesFromGCP(g.region, inv)
	return resources, err
}

// Execute implements Executor.
func (g *GCPProvider) Execute(ctx context.Context, r Resource, action, spec string) (string, error) {
	if IsProtected(r.Tags) {
		return "", fmt.Errorf("refusing to change protected resource %s", r.ID)
	}
	if err := g.ensure(ctx); err != nil {
		return "", err
	}
	if g.actions == nil {
		return "", fmt.Errorf("gcp actions not configured")
	}
	return executeGCP(ctx, g.actions, r, action, spec)
}

func (g *GCPProvider) ensure(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.loaded {
		return g.loadErr
	}
	if g.project == "" {
		g.loaded = true
		g.loadErr = fmt.Errorf("gcp project not configured (set GCP_PROJECT)")
		return g.loadErr
	}
	if g.connect == nil {
		g.loaded = true
		g.loadErr = fmt.Errorf("gcp client not configured")
		return g.loadErr
	}
	lister, actions, err := g.connect(ctx)
	g.loaded = true
	if err != nil {
		g.loadErr = err
		return err
	}
	g.lister = lister
	g.actions = actions
	return nil
}

// ResourcesFromGCP converts an inventory into resources, optionally filtered by region.
func ResourcesFromGCP(region string, inv GCPInventory) []Resource {
	var resources []Resource
	for _, inst := range inv.Instances {
		if inst == nil {
			continue
		}
		zone := LastPathSegment(inst.Zone)
		if !regionMatch(region, zone) {
			continue
		}
		resources = append(resources, Resource{
			ID:         fmt.Sprintf("%d", inst.Id),
			Name:       inst.Name,
			Provider:   "gcp",
			Type:       ResourceGCE,
			Region:     zone,
			Status:     inst.Status,
			Spec:       LastPathSegment(inst.MachineType),
			CostUSD:    GCEScanCost(inst.MachineType, inst.Status),
			CostSource: CostSourceEstimate,
			Tags:       inst.Labels,
			Metadata:   map[string]string{"zone": zone, "scope": "zone"},
		})
	}
	for _, disk := range inv.Disks {
		if disk == nil {
			continue
		}
		zone := LastPathSegment(disk.Zone)
		if !regionMatch(region, zone) {
			continue
		}
		status := "in-use"
		if len(disk.Users) == 0 {
			status = "unattached"
		}
		resources = append(resources, Resource{
			ID:         disk.Name,
			Name:       disk.Name,
			Provider:   "gcp",
			Type:       ResourceGCPDisk,
			Region:     zone,
			Status:     status,
			Spec:       LastPathSegment(disk.Type),
			CostUSD:    GCPDiskMonthly(disk.Type, disk.SizeGb),
			CostSource: CostSourceEstimate,
			Tags:       disk.Labels,
			Metadata:   map[string]string{"zone": zone, "size_gb": fmt.Sprintf("%d", disk.SizeGb)},
		})
	}
	for _, addr := range inv.Addresses {
		if addr == nil {
			continue
		}
		regionName := LastPathSegment(addr.Region)
		scope := "region"
		if regionName == "" {
			scope = "global"
			regionName = "global"
		}
		if scope != "global" && !regionMatch(region, regionName) {
			continue
		}
		status := "in-use"
		cost := 0.0
		if len(addr.Users) == 0 {
			status = "unattached"
			cost = GCPUnusedIPMonthly
		}
		resources = append(resources, Resource{
			ID:         addr.Name,
			Name:       firstNonEmpty(addr.Address, addr.Name),
			Provider:   "gcp",
			Type:       ResourceGCPIP,
			Region:     regionName,
			Status:     status,
			CostUSD:    cost,
			CostSource: CostSourceEstimate,
			Tags:       addr.Labels,
			Metadata:   map[string]string{"scope": scope, "region": regionName},
		})
	}
	for _, rule := range inv.ForwardingRules {
		if rule == nil {
			continue
		}
		regionName := LastPathSegment(rule.Region)
		scope := "region"
		if regionName == "" {
			scope = "global"
			regionName = "global"
		}
		if scope != "global" && !regionMatch(region, regionName) {
			continue
		}
		status := "active"
		if rule.BackendService == "" && rule.Target == "" {
			status = "idle"
		}
		resources = append(resources, Resource{
			ID:         rule.Name,
			Name:       rule.Name,
			Provider:   "gcp",
			Type:       ResourceGCPLB,
			Region:     regionName,
			Status:     status,
			Spec:       rule.LoadBalancingScheme,
			CostUSD:    ELBBaseMonthly,
			CostSource: CostSourceEstimate,
			Tags:       rule.Labels,
			Metadata:   map[string]string{"scope": scope, "region": regionName},
		})
	}
	for _, bucket := range inv.Buckets {
		if bucket == nil {
			continue
		}
		resources = append(resources, Resource{
			ID:         bucket.Name,
			Name:       bucket.Name,
			Provider:   "gcp",
			Type:       ResourceGCS,
			Region:     bucket.Location,
			Status:     "listed",
			Spec:       bucket.StorageClass,
			CostUSD:    0,
			CostSource: CostSourceEstimate,
			Tags:       bucket.Labels,
		})
	}
	for _, fn := range inv.Functions {
		if fn == nil {
			continue
		}
		resources = append(resources, Resource{
			ID:         fn.Name,
			Name:       LastPathSegment(fn.Name),
			Provider:   "gcp",
			Type:       ResourceCloudFunction,
			Region:     functionRegion(fn.Name),
			Status:     firstNonEmpty(fn.Status, "active"),
			Spec:       fn.Runtime,
			CostUSD:    0,
			CostSource: CostSourceEstimate,
			Tags:       fn.Labels,
			Metadata:   map[string]string{"memory_mb": fmt.Sprintf("%d", fn.AvailableMemoryMb)},
		})
	}
	for _, cluster := range inv.Clusters {
		if cluster == nil {
			continue
		}
		if !regionMatch(region, cluster.Location) {
			continue
		}
		resources = append(resources, Resource{
			ID:         cluster.Name,
			Name:       cluster.Name,
			Provider:   "gcp",
			Type:       ResourceGKE,
			Region:     cluster.Location,
			Status:     cluster.Status,
			Spec:       cluster.CurrentMasterVersion,
			CostUSD:    GKEManagementMonthly,
			CostSource: CostSourceEstimate,
			Tags:       cluster.ResourceLabels,
			Metadata:   map[string]string{"nodes": fmt.Sprintf("%d", cluster.CurrentNodeCount)},
		})
	}
	for _, mig := range inv.MIGs {
		if mig == nil {
			continue
		}
		zone := LastPathSegment(mig.Zone)
		loc := zone
		scope := "zone"
		if zone == "" {
			scope = "region"
			loc = LastPathSegment(mig.Region)
		}
		if !regionMatch(region, loc) {
			continue
		}
		resources = append(resources, Resource{
			ID:         mig.Name,
			Name:       mig.Name,
			Provider:   "gcp",
			Type:       ResourceMIG,
			Region:     loc,
			Status:     "active",
			Spec:       fmt.Sprintf("desired=%d", mig.TargetSize),
			CostUSD:    0,
			CostSource: CostSourceEstimate,
			Metadata: map[string]string{
				"scope":   scope,
				"desired": fmt.Sprintf("%d", mig.TargetSize),
				"zone":    zone,
			},
		})
	}
	return resources
}

func regionMatch(want, value string) bool {
	if want == "" || value == "" {
		return true
	}
	value = LastPathSegment(value)
	return value == want || strings.HasPrefix(value, want)
}

func functionRegion(name string) string {
	// projects/{p}/locations/{region}/functions/{name}
	parts := strings.Split(name, "/")
	for i, part := range parts {
		if part == "locations" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}
