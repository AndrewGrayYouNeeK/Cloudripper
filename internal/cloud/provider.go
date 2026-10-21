package cloud

import (
	"context"
	"errors"
	"fmt"
)

// ResourceType identifies a cloud resource category.
type ResourceType string

const (
	ResourceEC2           ResourceType = "ec2"
	ResourceRDS           ResourceType = "rds"
	ResourceEBS           ResourceType = "ebs"
	ResourceEIP           ResourceType = "eip"
	ResourceS3            ResourceType = "s3"
	ResourceELB           ResourceType = "elb"
	ResourceLambda        ResourceType = "lambda"
	ResourceEKS           ResourceType = "eks"
	ResourceASG           ResourceType = "asg"
	ResourceGCE           ResourceType = "gce"
	ResourceGCPDisk       ResourceType = "gcp-disk"
	ResourceGCPIP         ResourceType = "gcp-address"
	ResourceGCS           ResourceType = "gcs"
	ResourceGCPLB         ResourceType = "gcp-lb"
	ResourceCloudFunction ResourceType = "cloud-function"
	ResourceGKE           ResourceType = "gke"
	ResourceMIG           ResourceType = "mig"
	ResourceAzureVM       ResourceType = "azure-vm"
	ResourceAzureDisk     ResourceType = "azure-disk"
	ResourceAzureIP       ResourceType = "azure-ip"
	ResourceAzureStorage  ResourceType = "azure-storage"
	ResourceAzureLB       ResourceType = "azure-lb"
	ResourceAzureFunction ResourceType = "azure-function"
	ResourceAKS           ResourceType = "aks"
	ResourceUnknown       ResourceType = "unknown"
)

// Cost source labels stored on Resource.CostSource.
const (
	CostSourceEstimate = "estimate"
	CostSourceAWS      = "cost-explorer"
	CostSourceGCP      = "bigquery"
	CostSourceAzure    = "azure-cost"
)

// Resource represents a discovered cloud asset.
type Resource struct {
	ID         string
	Name       string
	Provider   string
	Type       ResourceType
	Region     string
	Status     string
	CostUSD    float64
	CostSource string
	Spec       string
	Tags       map[string]string
	Metadata   map[string]string
}

// DisplayName returns the human-facing name, falling back to the ID.
func (r Resource) DisplayName() string {
	if r.Name != "" {
		return r.Name
	}
	return r.ID
}

// Meta returns a metadata value.
func (r Resource) Meta(key string) string {
	if r.Metadata == nil {
		return ""
	}
	return r.Metadata[key]
}

// Provider scans a single cloud platform for resources.
type Provider interface {
	Name() string
	Scan(ctx context.Context) ([]Resource, error)
}

// Executor applies one mutation. Implementations must refuse protected resources.
type Executor interface {
	Provider() string
	Execute(ctx context.Context, r Resource, action, spec string) (string, error)
}

// MultiScanner aggregates resources across providers.
type MultiScanner struct {
	providers []Provider
}

// NewMultiScanner creates a scanner over the given providers.
func NewMultiScanner(providers ...Provider) *MultiScanner {
	return &MultiScanner{providers: providers}
}

// ScanAll collects resources from every configured provider.
// A non-nil error means one or more providers failed. Resources discovered by
// the providers that succeeded are still returned.
func (m *MultiScanner) ScanAll(ctx context.Context) ([]Resource, error) {
	var all []Resource
	var errs []error
	for _, p := range m.providers {
		resources, err := p.Scan(ctx)
		all = append(all, resources...)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p.Name(), err))
		}
	}
	if len(errs) > 0 {
		return all, errors.Join(errs...)
	}
	return all, nil
}

// IsCompute reports whether the type is a virtual machine we can stop,
// start, resize, or schedule.
func IsCompute(t ResourceType) bool {
	switch t {
	case ResourceEC2, ResourceGCE, ResourceAzureVM:
		return true
	default:
		return false
	}
}

// PutMeta sets a metadata key, allocating the map when needed.
func PutMeta(r *Resource, key, value string) {
	if r.Metadata == nil {
		r.Metadata = map[string]string{}
	}
	r.Metadata[key] = value
}
