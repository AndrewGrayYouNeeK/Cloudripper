package cost

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/cloud"
)

var billingTablePattern = regexp.MustCompile(`^[A-Za-z0-9_:\-]+\.[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+$`)

// Row is one grouped billing result.
type Row struct {
	Service  string
	Resource string
	Cost     float64
}

// Queryer runs a billing SQL statement. Tests supply a fake.
type Queryer interface {
	Query(ctx context.Context, sql string) ([]Row, error)
}

// BillingSQL builds a BigQuery standard-SQL statement for a billing export.
// The table must be project.dataset.table; it is rejected otherwise so the
// statement cannot be extended by the configuration value.
func BillingSQL(table string, start time.Time) (string, error) {
	table = strings.TrimSpace(table)
	table = strings.Trim(table, "`")
	if !billingTablePattern.MatchString(table) {
		return "", fmt.Errorf("GCP billing table %q must look like project.dataset.table", table)
	}
	start = start.UTC()
	return fmt.Sprintf(`SELECT
  IFNULL(service.description, "unknown") AS service,
  IFNULL(resource.name, "") AS resource,
  SUM(cost) + SUM(IFNULL((SELECT SUM(c.amount) FROM UNNEST(credits) AS c), 0)) AS net_cost
FROM `+"`%s`"+`
WHERE usage_start_time >= TIMESTAMP("%s")
GROUP BY service, resource`, table, start.Format(time.RFC3339)), nil
}

// GCPBilling reads a Cloud Billing BigQuery export.
type GCPBilling struct {
	Table string
	Query Queryer
	Now   func() time.Time
}

// Name implements Source.
func (g *GCPBilling) Name() string { return cloud.CostSourceGCP }

// Fetch implements Source.
func (g *GCPBilling) Fetch(ctx context.Context) (Snapshot, error) {
	if g == nil || g.Query == nil {
		return Snapshot{}, fmt.Errorf("bigquery client not configured")
	}
	now := time.Now
	if g.Now != nil {
		now = g.Now
	}
	start, _ := Window(now())
	sql, err := BillingSQL(g.Table, start)
	if err != nil {
		return Snapshot{}, err
	}
	rows, err := g.Query.Query(ctx, sql)
	if err != nil {
		return Snapshot{}, err
	}
	snap := Snapshot{Source: cloud.CostSourceGCP}
	for _, row := range rows {
		snap.TotalUSD += row.Cost
		if row.Service != "" {
			snap.Services = append(snap.Services, ServiceCost{Service: row.Service, USD: row.Cost})
		}
		if strings.TrimSpace(row.Resource) != "" {
			snap.Resources = append(snap.Resources, ResourceCost{
				ResourceID: row.Resource,
				Service:    row.Service,
				USD:        row.Cost,
			})
		}
	}
	return snap, nil
}

// StaticSource returns a fixed snapshot. Useful in tests and as a disabled source.
type StaticSource struct {
	SourceName string
	Snap       Snapshot
	Err        error
}

// Name implements Source.
func (s StaticSource) Name() string {
	if s.SourceName != "" {
		return s.SourceName
	}
	return s.Snap.Source
}

// Fetch implements Source.
func (s StaticSource) Fetch(context.Context) (Snapshot, error) {
	if s.Err != nil {
		return Snapshot{}, s.Err
	}
	return s.Snap, nil
}
