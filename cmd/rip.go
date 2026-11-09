package cmd

import (
	"fmt"

	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/cost"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/output"
	"github.com/spf13/cobra"
)

var ripCmd = &cobra.Command{
	Use:   "rip",
	Short: "Scan AWS, GCP, Azure, and Cloudflare and report what is running",
	Long: `Connects to configured clouds, discovers compute, storage, network,
Kubernetes, and Cloudflare developer-platform inventory, and overlays actual
spend when billing APIs respond. Priced resource costs stay on the built-in
estimate when a billing source is unavailable. Cloudflare inventory is
unpriced (cost unknown / n/a), not a $0 estimate.`,
	RunE: runRip,
}

func init() {
	rootCmd.AddCommand(ripCmd)
}

func runRip(cmd *cobra.Command, _ []string) error {
	cfg, res, err := loadResolution(cmd)
	if err != nil {
		return err
	}
	rows := rowsFromResources(res.Resources)
	if len(rows) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No resources found in the scanned region(s). Try --all-regions, or check credentials.")
		return nil
	}

	format, err := outputFormat()
	if err != nil {
		return err
	}
	if format == output.FormatTable {
		fmt.Fprint(cmd.OutOrStdout(), cost.Summary(res))
		fmt.Fprintf(cmd.OutOrStdout(), "Found %d resources:\n\n", len(rows))
	}

	headers, table := resourceTable(rows)
	payload := struct {
		DryRun          bool            `json:"dry_run"`
		EstimatedTotal  float64         `json:"estimated_total"`
		UnpricedCount   int             `json:"unpriced_count,omitempty"`
		ActualTotal     float64         `json:"actual_total"`
		ActualAvailable bool            `json:"actual_available"`
		Warnings        []string        `json:"warnings,omitempty"`
		Resources       []resourceRow   `json:"resources"`
		Snapshots       []cost.Snapshot `json:"snapshots,omitempty"`
	}{
		DryRun:          cfg.DryRun,
		EstimatedTotal:  res.EstimatedTotal,
		UnpricedCount:   res.UnpricedCount,
		ActualTotal:     res.ActualTotal,
		ActualAvailable: res.ActualAvailable,
		Warnings:        res.Warnings,
		Resources:       rows,
		Snapshots:       res.Snapshots,
	}
	if err := render(cmd, headers, table, payload); err != nil {
		return err
	}
	if cfg.DryRun && format == output.FormatTable {
		fmt.Fprintln(cmd.OutOrStdout(), "\n(dry-run mode — no changes made)")
	}
	return nil
}
