// Package demo loads the fictional inventory, spend, and card-export samples
// embedded in the binary. Nothing in this package opens a network connection.
package demo

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/cloud"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/cost"
)

// Banner is printed with every demo and sample-card report.
// It is a fixed string so a dataset edit cannot relabel the output as real.
const Banner = "FICTIONAL SAMPLE DATA. These resources, costs, and savings figures are made up. They are not your cloud spend and not real savings. No credentials were used and no network calls were made."

//go:embed data/inventory.json data/sample-card-export.csv
var files embed.FS

// Dataset is the embedded multi-cloud inventory and fictional spend snapshot.
type Dataset struct {
	Fictional  bool             `json:"fictional"`
	Disclaimer string           `json:"disclaimer"`
	Resources  []sampleResource `json:"resources"`
	Snapshots  []cost.Snapshot  `json:"snapshots"`
}

type sampleResource struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Provider   string            `json:"provider"`
	Type       string            `json:"type"`
	Region     string            `json:"region"`
	Status     string            `json:"status"`
	Spec       string            `json:"spec"`
	CostUSD    *float64          `json:"cost_usd"`
	CostSource string            `json:"cost_source"`
	Tags       map[string]string `json:"tags"`
}

// Load reads the embedded inventory and builds a cost resolution.
// Actual totals come only from the embedded snapshots. Estimated totals are
// the sum of priced rows, using the same rule as a live scan.
func Load() (Dataset, cost.Resolution, error) {
	raw, err := files.ReadFile("data/inventory.json")
	if err != nil {
		return Dataset{}, cost.Resolution{}, err
	}
	ds, err := parseDataset(raw)
	if err != nil {
		return Dataset{}, cost.Resolution{}, err
	}
	res, err := resolutionFrom(ds)
	if err != nil {
		return Dataset{}, cost.Resolution{}, err
	}
	return ds, res, nil
}

// CardExport returns the embedded card CSV. The bytes match
// docs/examples/sample-card-export.csv.
func CardExport() ([]byte, error) {
	return files.ReadFile("data/sample-card-export.csv")
}

// CardExportReader returns the embedded card CSV as a reader.
func CardExportReader() (io.Reader, error) {
	raw, err := CardExport()
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(raw), nil
}

func parseDataset(raw []byte) (Dataset, error) {
	var ds Dataset
	if err := json.Unmarshal(raw, &ds); err != nil {
		return Dataset{}, fmt.Errorf("sample inventory: %w", err)
	}
	if !ds.Fictional {
		return Dataset{}, fmt.Errorf("sample inventory is missing fictional:true and cannot be shown as a demo")
	}
	if !containsFictional(ds.Disclaimer) {
		return Dataset{}, fmt.Errorf("sample inventory disclaimer must say it is fictional")
	}
	if len(ds.Resources) == 0 {
		return Dataset{}, fmt.Errorf("sample inventory has no resources")
	}
	return ds, nil
}

func resolutionFrom(ds Dataset) (cost.Resolution, error) {
	resources := make([]cloud.Resource, 0, len(ds.Resources))
	for _, row := range ds.Resources {
		if row.ID == "" || row.Provider == "" || row.Type == "" {
			return cost.Resolution{}, fmt.Errorf("sample resource %q is missing id, provider, or type", row.ID)
		}
		source := row.CostSource
		if source == "" {
			source = cloud.CostSourceEstimate
		}
		res := cloud.Resource{
			ID:         row.ID,
			Name:       row.Name,
			Provider:   row.Provider,
			Type:       cloud.ResourceType(row.Type),
			Region:     row.Region,
			Status:     row.Status,
			Spec:       row.Spec,
			CostSource: source,
			Tags:       row.Tags,
		}
		if source == cloud.CostSourceUnpriced {
			resources = append(resources, res)
			continue
		}
		if row.CostUSD == nil {
			return cost.Resolution{}, fmt.Errorf("sample resource %q is priced but has no cost_usd", row.ID)
		}
		res.CostUSD = *row.CostUSD
		resources = append(resources, res)
	}
	out := cost.Resolution{
		Resources:      resources,
		Snapshots:      ds.Snapshots,
		EstimatedTotal: sumPriced(resources),
		UnpricedCount:  countUnpriced(resources),
	}
	if out.UnpricedCount > 0 {
		out.Warnings = append(out.Warnings, fmt.Sprintf(
			"estimated_total excludes %d unpriced resource(s); per-resource cost is unavailable for those rows (not a $0 estimate)",
			out.UnpricedCount,
		))
	}
	if len(ds.Snapshots) > 0 {
		out.ActualAvailable = true
		for _, snap := range ds.Snapshots {
			out.ActualTotal += snap.TotalUSD
		}
	}
	out.Warnings = append([]string{"fictional sample: account totals and per-resource amounts are made up"}, out.Warnings...)
	return out, nil
}

func sumPriced(resources []cloud.Resource) float64 {
	var total float64
	for _, r := range resources {
		if !r.CostPriced() {
			continue
		}
		total += r.CostUSD
	}
	return total
}

func countUnpriced(resources []cloud.Resource) int {
	n := 0
	for _, r := range resources {
		if !r.CostPriced() {
			n++
		}
	}
	return n
}

func containsFictional(text string) bool {
	return strings.Contains(text, "FICTIONAL") || strings.Contains(text, "fictional")
}
