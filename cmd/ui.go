package cmd

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/AndrewGrayYouNeeK/cloudripper/internal/apply"
	"github.com/AndrewGrayYouNeeK/cloudripper/internal/audit"
	"github.com/AndrewGrayYouNeeK/cloudripper/internal/cloud"
	"github.com/AndrewGrayYouNeeK/cloudripper/internal/config"
	"github.com/AndrewGrayYouNeeK/cloudripper/internal/cost"
	"github.com/AndrewGrayYouNeeK/cloudripper/internal/output"
	"github.com/spf13/cobra"
)

// batchConfirmer satisfies both apply and scale confirmation.
type batchConfirmer struct {
	in  io.Reader
	out io.Writer
	yes bool
}

func (c batchConfirmer) Confirm(summary string) (bool, error) {
	if c.yes {
		return true, nil
	}
	return apply.StdinConfirmer{In: c.in, Out: c.out}.Confirm(summary)
}

func newConfirmer(cmd *cobra.Command) (batchConfirmer, error) {
	if yes {
		return batchConfirmer{yes: true}, nil
	}
	in := cmd.InOrStdin()
	if !isTerminal(in) {
		return batchConfirmer{}, fmt.Errorf("refusing to change cloud resources without --yes because stdin is not a terminal")
	}
	return batchConfirmer{in: in, out: cmd.OutOrStdout()}, nil
}

func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func outputFormat() (output.Format, error) {
	value := outputFmt
	if !rootCmd.PersistentFlags().Changed("output") {
		if env := os.Getenv("CLOUDRIPPER_OUTPUT"); env != "" {
			value = env
		}
	}
	return output.Parse(value)
}

func auditLogger(cfg config.Config) *audit.Logger {
	if cfg.AuditLog == "" {
		return nil
	}
	return &audit.Logger{Path: cfg.AuditLog}
}

func commandContext(cmd *cobra.Command) context.Context {
	if cmd == nil || cmd.Context() == nil {
		return context.Background()
	}
	return cmd.Context()
}

func warnf(cmd *cobra.Command, format string, args ...any) {
	fmt.Fprintf(cmd.ErrOrStderr(), "warning: "+format+"\n", args...)
}

type resourceRow struct {
	Provider   string            `json:"provider"`
	Type       string            `json:"type"`
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Region     string            `json:"region"`
	Status     string            `json:"status"`
	Spec       string            `json:"spec,omitempty"`
	CostUSD    float64           `json:"cost_usd"`
	CostSource string            `json:"cost_source,omitempty"`
	Tags       map[string]string `json:"tags,omitempty"`
}

func rowsFromResources(resources []cloud.Resource) []resourceRow {
	rows := make([]resourceRow, 0, len(resources))
	for _, r := range resources {
		rows = append(rows, resourceRow{
			Provider:   r.Provider,
			Type:       string(r.Type),
			ID:         r.ID,
			Name:       r.DisplayName(),
			Region:     r.Region,
			Status:     r.Status,
			Spec:       r.Spec,
			CostUSD:    r.CostUSD,
			CostSource: r.CostSource,
			Tags:       r.Tags,
		})
	}
	return rows
}

func resourceTable(rows []resourceRow) ([]string, [][]string) {
	headers := []string{"PROVIDER", "TYPE", "ID", "NAME", "REGION", "STATUS", "SPEC", "USD", "SOURCE"}
	out := make([][]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, []string{
			row.Provider,
			row.Type,
			row.ID,
			row.Name,
			row.Region,
			row.Status,
			row.Spec,
			fmt.Sprintf("%.2f", row.CostUSD),
			row.CostSource,
		})
	}
	return headers, out
}

func render(cmd *cobra.Command, headers []string, rows [][]string, payload any) error {
	format, err := outputFormat()
	if err != nil {
		return err
	}
	w := cmd.OutOrStdout()
	switch format {
	case output.FormatJSON:
		return output.WriteJSON(w, payload)
	case output.FormatCSV:
		return output.WriteCSV(w, headers, rows)
	case output.FormatTable:
		return output.WriteTable(w, headers, rows)
	default:
		return fmt.Errorf("unhandled output format %q", format)
	}
}

func loadResolution(cmd *cobra.Command) (config.Config, cost.Resolution, error) {
	cfg := loadConfig()
	ctx := commandContext(cmd)
	resources, err := rt.scan(ctx, cfg)
	if err != nil && len(resources) == 0 {
		return cfg, cost.Resolution{}, fmt.Errorf("scan failed: %w", err)
	}
	if err != nil {
		warnf(cmd, "%v", err)
	}
	res, cerr := rt.costs(ctx, cfg, resources)
	if cerr != nil {
		warnf(cmd, "%v", cerr)
		res = cost.Resolution{Resources: resources, EstimatedTotal: sumCost(resources)}
	}
	for _, warning := range res.Warnings {
		warnf(cmd, "%s", warning)
	}
	return cfg, res, nil
}

func sumCost(resources []cloud.Resource) float64 {
	var total float64
	for _, r := range resources {
		total += r.CostUSD
	}
	return total
}
