package cmd

import (
	"fmt"

	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/cost"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/output"
	"github.com/spf13/cobra"
)

var costCmd = &cobra.Command{
	Use:   "cost",
	Short: "Show estimated inventory cost and actual trailing spend",
	Long: `Pulls AWS Cost Explorer, a GCP Cloud Billing BigQuery export, and Azure Cost
Management when those sources are configured. A source that fails is reported
as a warning and the command keeps the built-in estimate for that cloud.

Actual totals are account spend over the trailing 30 days. Estimated totals
are the sum of per-resource price heuristics. They are not the same number.`,
	RunE: runCost,
}

func init() {
	rootCmd.AddCommand(costCmd)
}

func runCost(cmd *cobra.Command, _ []string) error {
	_, res, err := loadResolution(cmd)
	if err != nil {
		return err
	}
	format, err := outputFormat()
	if err != nil {
		return err
	}
	if format == output.FormatTable {
		fmt.Fprint(cmd.OutOrStdout(), cost.Summary(res))
		fmt.Fprintln(cmd.OutOrStdout(), "Resource lines:")
	}
	rows := rowsFromResources(res.Resources)
	headers, table := resourceTable(rows)
	return render(cmd, headers, table, res)
}
