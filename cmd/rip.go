package cmd

import (
	"fmt"

	"github.com/AndrewGrayYouNeeK/cloudripper/internal/cost"
	"github.com/AndrewGrayYouNeeK/cloudripper/internal/output"
	"github.com/spf13/cobra"
)

var ripCmd = &cobra.Command{
	Use:   "rip",
	Short: "Scan AWS, GCP, and Azure and report what is running",
	Long: `Connects to configured clouds, discovers compute, storage, network, and
Kubernetes inventory, and overlays actual spend when billing APIs respond.
Resource costs stay on the built-in estimate when a billing source is unavailable.`,
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
		fmt.Fprintln(cmd.OutOrStdout(), "No resources found. Either your clouds are empty or credentials need setup.")
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
		ActualTotal     float64         `json:"actual_total"`
		ActualAvailable bool            `json:"actual_available"`
		Warnings        []string        `json:"warnings,omitempty"`
		Resources       []resourceRow   `json:"resources"`
		Snapshots       []cost.Snapshot `json:"snapshots,omitempty"`
	}{
		DryRun:          cfg.DryRun,
		EstimatedTotal:  res.EstimatedTotal,
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
