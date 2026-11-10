package cmd

import (
	"fmt"

	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/cost"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/optimizer"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/output"
	"github.com/spf13/cobra"
)

const htmlActualNote = "Unpriced per-resource costs are shown as n/a, not as $0. When billing data was not returned, actual spend is unavailable, not $0."

func writeSpendHTML(cmd *cobra.Command, title string, res cost.Resolution, headers []string, rows [][]string, extra []string) error {
	notes := []string{
		fmt.Sprintf("Estimated resource total: $%.2f", res.EstimatedTotal),
		"Actual spend: " + actualDisplay(res.ActualAvailable, res.ActualTotal),
		htmlActualNote,
	}
	if res.UnpricedCount > 0 {
		notes = append(notes, fmt.Sprintf("%d resource row(s) are unpriced and excluded from the estimated total.", res.UnpricedCount))
	}
	notes = append(notes, extra...)
	notes = append(notes, res.Warnings...)
	return output.WriteHTML(cmd.OutOrStdout(), output.HTMLReport{
		Title: title,
		Notes: notes,
		Sections: []output.HTMLSection{{
			Heading: "Resources",
			Headers: headers,
			Rows:    rows,
		}},
	})
}

func writeOptimizeHTML(cmd *cobra.Command, res cost.Resolution, result optimizer.Result, dryRun bool, heading string, headers []string, rows [][]string) error {
	notes := []string{
		fmt.Sprintf("Estimated resource total: $%.2f", res.EstimatedTotal),
		"Actual spend: " + actualDisplay(res.ActualAvailable, res.ActualTotal),
		htmlActualNote,
		fmt.Sprintf("Optimizer monthly total: $%.2f. Potential savings: $%.2f. These are heuristics, not measured billing reductions.", result.TotalMonthlyCost, result.PotentialSavings),
	}
	if dryRun {
		notes = append(notes, "Dry-run: no changes applied.")
	}
	notes = append(notes, res.Warnings...)
	return output.WriteHTML(cmd.OutOrStdout(), output.HTMLReport{
		Title: "CloudRipper optimize",
		Notes: notes,
		Sections: []output.HTMLSection{{
			Heading: heading,
			Headers: headers,
			Rows:    rows,
		}},
	})
}
