package cloud

import "context"

// ResourceType identifies a cloud resource category.
type ResourceType string

const (
	ResourceEC2      ResourceType = "ec2"
	ResourceRDS      ResourceType = "rds"
	ResourceGCE      ResourceType = "gce"
	ResourceUnknown  ResourceType = "unknown"
)

// Resource represents a discovered cloud asset.
type Resource struct {
	ID       string
	Name     string
	Provider string
	Type     ResourceType
	Region   string
	Status   string
	CostUSD  float64
	Tags     map[string]string
}

// Provider scans a single cloud platform for resources.
type Provider interface {
	Name() string
	Scan(ctx context.Context) ([]Resource, error)
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
func (m *MultiScanner) ScanAll(ctx context.Context) ([]Resource, error) {
	var all []Resource
	for _, p := range m.providers {
		resources, err := p.Scan(ctx)
		if err != nil {
			return nil, err
		}
		all = append(all, resources...)
	}
	return all, nil
}
