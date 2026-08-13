package cloud

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
)

// GCPProvider scans Google Cloud resources.
type GCPProvider struct {
	project string
	region  string
}

// NewGCPProvider creates a GCP scanner for the given project and region.
func NewGCPProvider(project, region string) *GCPProvider {
	return &GCPProvider{project: project, region: region}
}

func (g *GCPProvider) Name() string { return "gcp" }

func (g *GCPProvider) Scan(ctx context.Context) ([]Resource, error) {
	if g.project == "" {
		return nil, fmt.Errorf("gcp project not configured (set GCP_PROJECT)")
	}

	svc, err := compute.NewService(ctx, option.WithScopes(compute.ComputeScope))
	if err != nil {
		return nil, fmt.Errorf("gcp compute client: %w", err)
	}

	var resources []Resource
	req := svc.Instances.AggregatedList(g.project)
	err = req.Pages(ctx, func(page *compute.InstanceAggregatedList) error {
		for zone, scoped := range page.Items {
			if scoped.Instances == nil {
				continue
			}
			zoneName := zoneNameFromURL(zone)
			if g.region != "" && !strings.HasPrefix(zoneName, g.region) {
				continue
			}
			for _, inst := range scoped.Instances {
				resources = append(resources, Resource{
					ID:       fmt.Sprintf("%d", inst.Id),
					Name:     inst.Name,
					Provider: "gcp",
					Type:     ResourceGCE,
					Region:   zoneName,
					Status:   inst.Status,
					CostUSD:  estimateGCECost(inst.MachineType),
					Tags:     inst.Labels,
				})
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("gcp instance scan: %w", err)
	}

	return resources, nil
}

func zoneNameFromURL(url string) string {
	parts := strings.Split(url, "/")
	if len(parts) == 0 {
		return url
	}
	return parts[len(parts)-1]
}

func estimateGCECost(machineTypeURL string) float64 {
	machineType := zoneNameFromURL(machineTypeURL)
	estimates := map[string]float64{
		"e2-micro":       6.0,
		"e2-small":       12.0,
		"e2-medium":      24.0,
		"n1-standard-1":  48.0,
		"n1-standard-2":  96.0,
		"n1-standard-4":  192.0,
	}
	if cost, ok := estimates[machineType]; ok {
		return cost
	}
	return 45.0
}
