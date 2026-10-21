package cost

import (
	"context"
	"fmt"
	"strconv"

	"google.golang.org/api/bigquery/v2"
	"google.golang.org/api/option"
)

type bigQueryRunner struct {
	svc     *bigquery.Service
	project string
}

// NewGCPQueryer builds a BigQuery client for the billing export project.
func NewGCPQueryer(ctx context.Context, project string) (Queryer, error) {
	if project == "" {
		return nil, fmt.Errorf("gcp project not configured")
	}
	svc, err := bigquery.NewService(ctx, option.WithScopes(bigquery.BigqueryScope))
	if err != nil {
		return nil, err
	}
	return &bigQueryRunner{svc: svc, project: project}, nil
}

func (b *bigQueryRunner) Query(ctx context.Context, sql string) ([]Row, error) {
	legacy := false
	resp, err := b.svc.Jobs.Query(b.project, &bigquery.QueryRequest{
		Query:        sql,
		UseLegacySql: &legacy,
		TimeoutMs:    60000,
	}).Context(ctx).Do()
	if err != nil {
		return nil, err
	}
	if resp == nil || !resp.JobComplete {
		return nil, fmt.Errorf("bigquery billing query did not finish")
	}
	if len(resp.Errors) > 0 && resp.Errors[0] != nil {
		return nil, fmt.Errorf("bigquery: %s", resp.Errors[0].Message)
	}
	rows := make([]Row, 0, len(resp.Rows))
	for _, row := range resp.Rows {
		if row == nil || len(row.F) < 3 || row.F[2] == nil {
			continue
		}
		amount, err := strconv.ParseFloat(fmt.Sprint(cell(row.F[2])), 64)
		if err != nil {
			return nil, fmt.Errorf("billing cost %v: %w", row.F[2].V, err)
		}
		rows = append(rows, Row{
			Service:  fmt.Sprint(cell(row.F[0])),
			Resource: fmt.Sprint(cell(row.F[1])),
			Cost:     amount,
		})
	}
	return rows, nil
}

func cell(c *bigquery.TableCell) any {
	if c == nil || c.V == nil {
		return ""
	}
	return c.V
}
