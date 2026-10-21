package cloud

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// AzureVM is one virtual machine.
type AzureVM struct {
	ID       string
	Name     string
	Location string
	Group    string
	Size     string
	Status   string
	Tags     map[string]string
}

// AzureDisk is one managed disk.
type AzureDisk struct {
	ID       string
	Name     string
	Location string
	Group    string
	SizeGB   int64
	Status   string
	Tags     map[string]string
}

// AzureAddress is one public IP.
type AzureAddress struct {
	ID       string
	Name     string
	Location string
	Group    string
	Status   string
	Tags     map[string]string
}

// AzureStorage is one storage account.
type AzureStorage struct {
	ID       string
	Name     string
	Location string
	Group    string
	SKU      string
	Tags     map[string]string
}

// AzureLB is one load balancer.
type AzureLB struct {
	ID       string
	Name     string
	Location string
	Group    string
	Status   string
	Tags     map[string]string
}

// AzureFunction is one function app.
type AzureFunction struct {
	ID       string
	Name     string
	Location string
	Group    string
	Status   string
	Tags     map[string]string
}

// AzureCluster is one AKS cluster.
type AzureCluster struct {
	ID       string
	Name     string
	Location string
	Group    string
	Status   string
	Version  string
	Tags     map[string]string
}

// AzureInventory is the normalized Azure scan result.
type AzureInventory struct {
	VMs           []AzureVM
	Disks         []AzureDisk
	Addresses     []AzureAddress
	Storage       []AzureStorage
	LoadBalancers []AzureLB
	Functions     []AzureFunction
	Clusters      []AzureCluster
}

// AzureLister lists a subscription.
type AzureLister interface {
	Inventory(ctx context.Context) (AzureInventory, error)
}

// AzureActions mutates Azure resources.
type AzureActions interface {
	DeallocateVM(ctx context.Context, group, name string) error
	StartVM(ctx context.Context, group, name string) error
	DeleteVM(ctx context.Context, group, name string) error
	ResizeVM(ctx context.Context, group, name, size string) error
	TagVM(ctx context.Context, group, name, key, value string) error
	DeleteDisk(ctx context.Context, group, name string) error
	DeleteAddress(ctx context.Context, group, name string) error
	DeleteLoadBalancer(ctx context.Context, group, name string) error
}

// AzureProvider scans and mutates one subscription.
type AzureProvider struct {
	subscription string
	group        string
	lister       AzureLister
	actions      AzureActions
	connect      func(ctx context.Context) (AzureLister, AzureActions, error)

	mu      sync.Mutex
	loaded  bool
	loadErr error
}

// NewAzureProvider authenticates with DefaultAzureCredential on first use.
func NewAzureProvider(subscription, group string) *AzureProvider {
	p := &AzureProvider{subscription: subscription, group: group}
	p.connect = func(ctx context.Context) (AzureLister, AzureActions, error) {
		return connectAzure(ctx, subscription, group)
	}
	return p
}

// NewAzureProviderWith wires fakes.
func NewAzureProviderWith(subscription, group string, lister AzureLister, actions AzureActions) *AzureProvider {
	return &AzureProvider{subscription: subscription, group: group, lister: lister, actions: actions, loaded: true}
}

// Name implements Provider.
func (a *AzureProvider) Name() string { return "azure" }

// Provider implements Executor.
func (a *AzureProvider) Provider() string { return "azure" }

// Scan implements Provider.
func (a *AzureProvider) Scan(ctx context.Context) ([]Resource, error) {
	if err := a.ensure(ctx); err != nil {
		return nil, err
	}
	inv, err := a.lister.Inventory(ctx)
	return ResourcesFromAzure(inv), err
}

// Execute implements Executor.
func (a *AzureProvider) Execute(ctx context.Context, r Resource, action, spec string) (string, error) {
	if IsProtected(r.Tags) {
		return "", fmt.Errorf("refusing to change protected resource %s", r.ID)
	}
	if err := a.ensure(ctx); err != nil {
		return "", err
	}
	if a.actions == nil {
		return "", fmt.Errorf("azure actions not configured")
	}
	return executeAzure(ctx, a.actions, r, action, spec)
}

func (a *AzureProvider) ensure(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.loaded {
		return a.loadErr
	}
	if a.subscription == "" {
		a.loaded = true
		a.loadErr = fmt.Errorf("azure subscription not configured (set AZURE_SUBSCRIPTION_ID)")
		return a.loadErr
	}
	if a.connect == nil {
		a.loaded = true
		a.loadErr = fmt.Errorf("azure client not configured")
		return a.loadErr
	}
	lister, actions, err := a.connect(ctx)
	a.loaded = true
	if err != nil {
		a.loadErr = err
		return err
	}
	a.lister = lister
	a.actions = actions
	return nil
}

// ResourcesFromAzure converts a normalized inventory into resources.
func ResourcesFromAzure(inv AzureInventory) []Resource {
	var resources []Resource
	for _, vm := range inv.VMs {
		cost := estimateAzureVMCost(vm.Size)
		if isAzureStopped(vm.Status) {
			cost = 0
		}
		resources = append(resources, Resource{
			ID:         firstNonEmpty(vm.ID, vm.Name),
			Name:       vm.Name,
			Provider:   "azure",
			Type:       ResourceAzureVM,
			Region:     vm.Location,
			Status:     vm.Status,
			Spec:       vm.Size,
			CostUSD:    cost,
			CostSource: CostSourceEstimate,
			Tags:       vm.Tags,
			Metadata:   map[string]string{"resource_group": vm.Group},
		})
	}
	for _, disk := range inv.Disks {
		resources = append(resources, Resource{
			ID:         firstNonEmpty(disk.ID, disk.Name),
			Name:       disk.Name,
			Provider:   "azure",
			Type:       ResourceAzureDisk,
			Region:     disk.Location,
			Status:     disk.Status,
			CostUSD:    AzureDiskMonthly(disk.SizeGB),
			CostSource: CostSourceEstimate,
			Tags:       disk.Tags,
			Metadata:   map[string]string{"resource_group": disk.Group, "size_gb": fmt.Sprintf("%d", disk.SizeGB)},
		})
	}
	for _, addr := range inv.Addresses {
		cost := 0.0
		if strings.EqualFold(addr.Status, "unattached") {
			cost = AzureUnusedIPMonthly
		}
		resources = append(resources, Resource{
			ID:         firstNonEmpty(addr.ID, addr.Name),
			Name:       addr.Name,
			Provider:   "azure",
			Type:       ResourceAzureIP,
			Region:     addr.Location,
			Status:     addr.Status,
			CostUSD:    cost,
			CostSource: CostSourceEstimate,
			Tags:       addr.Tags,
			Metadata:   map[string]string{"resource_group": addr.Group},
		})
	}
	for _, account := range inv.Storage {
		resources = append(resources, Resource{
			ID:         firstNonEmpty(account.ID, account.Name),
			Name:       account.Name,
			Provider:   "azure",
			Type:       ResourceAzureStorage,
			Region:     account.Location,
			Status:     "listed",
			Spec:       account.SKU,
			CostUSD:    0,
			CostSource: CostSourceEstimate,
			Tags:       account.Tags,
			Metadata:   map[string]string{"resource_group": account.Group},
		})
	}
	for _, lb := range inv.LoadBalancers {
		cost := 0.0
		if strings.EqualFold(lb.Status, "idle") {
			cost = AzureLBMonthly
		} else {
			cost = AzureLBMonthly
		}
		resources = append(resources, Resource{
			ID:         firstNonEmpty(lb.ID, lb.Name),
			Name:       lb.Name,
			Provider:   "azure",
			Type:       ResourceAzureLB,
			Region:     lb.Location,
			Status:     lb.Status,
			CostUSD:    cost,
			CostSource: CostSourceEstimate,
			Tags:       lb.Tags,
			Metadata:   map[string]string{"resource_group": lb.Group},
		})
	}
	for _, fn := range inv.Functions {
		resources = append(resources, Resource{
			ID:         firstNonEmpty(fn.ID, fn.Name),
			Name:       fn.Name,
			Provider:   "azure",
			Type:       ResourceAzureFunction,
			Region:     fn.Location,
			Status:     firstNonEmpty(fn.Status, "active"),
			CostUSD:    0,
			CostSource: CostSourceEstimate,
			Tags:       fn.Tags,
			Metadata:   map[string]string{"resource_group": fn.Group},
		})
	}
	for _, cluster := range inv.Clusters {
		resources = append(resources, Resource{
			ID:         firstNonEmpty(cluster.ID, cluster.Name),
			Name:       cluster.Name,
			Provider:   "azure",
			Type:       ResourceAKS,
			Region:     cluster.Location,
			Status:     cluster.Status,
			Spec:       cluster.Version,
			CostUSD:    0,
			CostSource: CostSourceEstimate,
			Tags:       cluster.Tags,
			Metadata:   map[string]string{"resource_group": cluster.Group},
		})
	}
	return resources
}

func isAzureStopped(status string) bool {
	switch strings.ToLower(status) {
	case "stopped", "deallocated", "stopping":
		return true
	default:
		return false
	}
}
