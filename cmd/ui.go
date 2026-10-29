package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

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
	writeSkips(cmd.ErrOrStderr(), cfg)
	ctx := commandContext(cmd)
	resources, err := rt.scan(ctx, cfg)
	if err != nil && len(resources) == 0 {
		return cfg, cost.Resolution{}, fmt.Errorf("scan failed: %s", humanizeJoined(err))
	}
	if err != nil {
		writeScanProblems(cmd.ErrOrStderr(), err)
	}
	res, cerr := rt.costs(ctx, cfg, resources)
	if cerr != nil {
		warnf(cmd, "%v", cerr)
		res = cost.Resolution{Resources: resources, EstimatedTotal: sumCost(resources)}
	}
	format, ferr := outputFormat()
	if ferr != nil {
		return cfg, res, ferr
	}
	// JSON includes warnings in the payload. Table and CSV print each warning once on stderr.
	if format != output.FormatJSON {
		for _, warning := range res.Warnings {
			warnf(cmd, "%s", warning)
		}
	}
	return cfg, res, nil
}

func writeSkips(w io.Writer, cfg config.Config) {
	for _, line := range skippedProviders(cfg) {
		fmt.Fprintln(w, line)
	}
}

func skippedProviders(cfg config.Config) []string {
	var lines []string
	if strings.TrimSpace(cfg.GCPProject) == "" {
		lines = append(lines, "skipped: GCP project is not set (set GCP_PROJECT or --gcp-project)")
	}
	if strings.TrimSpace(cfg.AzureSubscription) == "" {
		lines = append(lines, "skipped: Azure subscription is not set (set AZURE_SUBSCRIPTION_ID or --azure-subscription)")
	}
	return lines
}

func writeScanProblems(w io.Writer, err error) {
	if err == nil {
		return
	}
	for _, line := range strings.Split(err.Error(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fmt.Fprintln(w, humanizeScanLine(line))
	}
}

func humanizeJoined(err error) string {
	if err == nil {
		return ""
	}
	var parts []string
	for _, line := range strings.Split(err.Error(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts = append(parts, strings.TrimPrefix(humanizeScanLine(line), "warning: "))
	}
	return strings.Join(parts, "; ")
}

func humanizeScanLine(line string) string {
	lower := strings.ToLower(line)
	switch {
	case strings.Contains(lower, "gcp") && (strings.Contains(lower, "credential") || strings.Contains(lower, "could not find default")):
		return "skipped: GCP credentials are not available (set application default credentials)"
	case strings.Contains(lower, "azure") && (strings.Contains(lower, "credential") || strings.Contains(lower, "defaultazurecredential")):
		return "skipped: Azure credentials are not available (set Azure CLI or environment credentials)"
	default:
		return "warning: " + line
	}
}

func sumCost(resources []cloud.Resource) float64 {
	var total float64
	for _, r := range resources {
		total += r.CostUSD
	}
	return total
}
