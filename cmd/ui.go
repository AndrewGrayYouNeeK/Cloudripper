package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/apply"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/audit"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/cloud"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/config"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/cost"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/output"
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
	CostUSD    *float64          `json:"cost_usd"`
	CostSource string            `json:"cost_source,omitempty"`
	Tags       map[string]string `json:"tags,omitempty"`
}

func rowsFromResources(resources []cloud.Resource) []resourceRow {
	rows := make([]resourceRow, 0, len(resources))
	for _, r := range resources {
		row := resourceRow{
			Provider:   r.Provider,
			Type:       string(r.Type),
			ID:         r.ID,
			Name:       r.DisplayName(),
			Region:     r.Region,
			Status:     r.Status,
			Spec:       r.Spec,
			CostSource: r.CostSource,
			Tags:       r.Tags,
		}
		if r.CostPriced() {
			v := r.CostUSD
			row.CostUSD = &v
		}
		rows = append(rows, row)
	}
	return rows
}

// scanDocument is the shared `rip --output json` and `cost --output json` shape.
// Resource fields are snake_case. Unpriced rows keep cost_usd null.
type scanDocument struct {
	DryRun          bool            `json:"dry_run"`
	EstimatedTotal  float64         `json:"estimated_total"`
	UnpricedCount   int             `json:"unpriced_count,omitempty"`
	ActualTotal     float64         `json:"actual_total"`
	ActualAvailable bool            `json:"actual_available"`
	Warnings        []string        `json:"warnings,omitempty"`
	Resources       []resourceRow   `json:"resources"`
	Snapshots       []cost.Snapshot `json:"snapshots,omitempty"`
}

func scanPayload(cfg config.Config, res cost.Resolution) scanDocument {
	rows := rowsFromResources(res.Resources)
	for i := range rows {
		if rows[i].CostUSD == nil {
			continue
		}
		v := cost.RoundCents(*rows[i].CostUSD)
		rows[i].CostUSD = &v
	}
	return scanDocument{
		DryRun:          cfg.DryRun,
		EstimatedTotal:  cost.RoundCents(res.EstimatedTotal),
		UnpricedCount:   res.UnpricedCount,
		ActualTotal:     cost.RoundCents(res.ActualTotal),
		ActualAvailable: res.ActualAvailable,
		Warnings:        res.Warnings,
		Resources:       rows,
		Snapshots:       roundSnapshotMoney(res.Snapshots),
	}
}

func roundSnapshotMoney(in []cost.Snapshot) []cost.Snapshot {
	if len(in) == 0 {
		return in
	}
	out := make([]cost.Snapshot, len(in))
	for i, snap := range in {
		snap.TotalUSD = cost.RoundCents(snap.TotalUSD)
		if len(snap.Services) > 0 {
			services := make([]cost.ServiceCost, len(snap.Services))
			for j, svc := range snap.Services {
				svc.USD = cost.RoundCents(svc.USD)
				services[j] = svc
			}
			snap.Services = services
		}
		if len(snap.Resources) > 0 {
			resources := make([]cost.ResourceCost, len(snap.Resources))
			for j, row := range snap.Resources {
				row.USD = cost.RoundCents(row.USD)
				resources[j] = row
			}
			snap.Resources = resources
		}
		out[i] = snap
	}
	return out
}

func resourceTable(rows []resourceRow) ([]string, [][]string) {
	headers := []string{"PROVIDER", "TYPE", "ID", "NAME", "REGION", "STATUS", "SPEC", "USD", "SOURCE"}
	out := make([][]string, 0, len(rows))
	for _, row := range rows {
		usd := "n/a"
		if row.CostUSD != nil {
			usd = fmt.Sprintf("%.2f", *row.CostUSD)
		} else if row.CostSource == "" {
			usd = "0.00"
		}
		out = append(out, []string{
			row.Provider,
			row.Type,
			row.ID,
			row.Name,
			row.Region,
			row.Status,
			row.Spec,
			usd,
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
		return cfg, cost.Resolution{}, friendlyScanError(err)
	}
	if err != nil {
		writeScanProblems(cmd.ErrOrStderr(), err)
	}
	res, cerr := rt.costs(ctx, cfg, resources)
	if cerr != nil {
		warnf(cmd, "%v", cerr)
		unpriced := 0
		for _, r := range resources {
			if !r.CostPriced() {
				unpriced++
			}
		}
		res = cost.Resolution{Resources: resources, EstimatedTotal: sumCost(resources), UnpricedCount: unpriced}
		if unpriced > 0 {
			res.Warnings = []string{fmt.Sprintf("estimated_total excludes %d unpriced resource(s); per-resource cost is unavailable for those rows (not a $0 estimate)", unpriced)}
		}
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
	account := strings.TrimSpace(cfg.CloudflareAccountID)
	token := strings.TrimSpace(cfg.CloudflareAPIToken)
	switch {
	case account == "" && token == "":
		lines = append(lines, "skipped: Cloudflare is not set (set CLOUDFLARE_ACCOUNT_ID and CLOUDFLARE_API_TOKEN)")
	case account == "":
		lines = append(lines, "skipped: Cloudflare account id is not set (set CLOUDFLARE_ACCOUNT_ID or --cloudflare-account)")
	case token == "":
		lines = append(lines, "skipped: Cloudflare API token is not set (set CLOUDFLARE_API_TOKEN)")
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
	case strings.Contains(lower, "cloudflare") && (strings.Contains(lower, "api token not configured") || strings.Contains(lower, "account id not configured") || strings.Contains(lower, "permission denied") || strings.Contains(lower, "unauthorized")):
		return "skipped: Cloudflare credentials are not available or lack permission (set CLOUDFLARE_ACCOUNT_ID and a read-scoped CLOUDFLARE_API_TOKEN)"
	default:
		return "warning: " + line
	}
}

func sumCost(resources []cloud.Resource) float64 {
	var total float64
	for _, r := range resources {
		if !r.CostPriced() {
			continue
		}
		total += r.CostUSD
	}
	return total
}
