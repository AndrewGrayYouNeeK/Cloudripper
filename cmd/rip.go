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
	format, err := outputFormat()
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		if format == output.FormatHTML {
			return output.WriteHTML(cmd.OutOrStdout(), output.HTMLReport{
				Title: "CloudRipper rip",
				Notes: []string{"No resources found in the scanned region(s). Try --all-regions, or check credentials."},
			})
		}
		fmt.Fprintln(cmd.OutOrStdout(), "No resources found in the scanned region(s). Try --all-regions, or check credentials.")
		return nil
	}

	if format == output.FormatTable {
		fmt.Fprint(cmd.OutOrStdout(), cost.Summary(res))
		fmt.Fprintf(cmd.OutOrStdout(), "Found %d resources:\n\n", len(rows))
	}

	headers, table := resourceTable(rows)
	if format == output.FormatHTML {
		extra := []string{}
		if cfg.DryRun {
			extra = append(extra, "Dry-run: no changes made.")
		}
		return writeSpendHTML(cmd, "CloudRipper rip", res, headers, table, extra)
	}
	if err := render(cmd, headers, table, scanPayload(cfg, res)); err != nil {
		return err
	}
	if cfg.DryRun && format == output.FormatTable {
		fmt.Fprintln(cmd.OutOrStdout(), "\n(dry-run mode — no changes made)")
	}
	return nil
}
