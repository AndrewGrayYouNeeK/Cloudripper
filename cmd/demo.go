package cmd

import (
	"fmt"
	"strings"

	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/cost"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/demo"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/optimizer"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/output"
	"github.com/spf13/cobra"
)

const demoSavingsNote = "Potential savings are the optimizer's heuristic on this fictional inventory. They are not real savings, and the demo does not change any cloud resource."

var demoCmd = &cobra.Command{
	Use:   "demo",
	Short: "Show rip, cost, and optimize output for a fictional sample",
	Long: `Runs the inventory, spend, and recommendation reports against a fictional
multi-cloud sample embedded in the binary (AWS, GCP, Azure, and Cloudflare).

The command does not read credentials and does not make network calls.
Every amount is made up. It is not your cloud spend and not real savings.

--output table, json, csv, or html. HTML is one self-contained file.`,
	RunE: runDemo,
}

func init() {
	rootCmd.AddCommand(demoCmd)
}

func runDemo(cmd *cobra.Command, _ []string) error {
	_, res, err := demo.Load()
	if err != nil {
		return err
	}
	result := optimizer.Analyze(res.Resources)
	format, err := outputFormat()
	if err != nil {
		return err
	}
	switch format {
	case output.FormatJSON:
		fmt.Fprintln(cmd.ErrOrStderr(), demo.Banner)
		return output.WriteJSON(cmd.OutOrStdout(), demoDocument(res, result))
	case output.FormatCSV:
		fmt.Fprintln(cmd.ErrOrStderr(), demo.Banner)
		headers, rows := demoCSV(res, result)
		return output.WriteCSV(cmd.OutOrStdout(), headers, rows)
	case output.FormatHTML:
		return output.WriteHTML(cmd.OutOrStdout(), demoHTML(res, result))
	case output.FormatTable:
		return writeDemoTable(cmd, res, result)
	default:
		return fmt.Errorf("unhandled output format %q", format)
	}
}

type demoRec struct {
	Action        string  `json:"action"`
	Provider      string  `json:"provider"`
	Type          string  `json:"type"`
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Savings       float64 `json:"savings"`
	SuggestedSpec string  `json:"suggested_spec,omitempty"`
	Reason        string  `json:"reason"`
	Fictional     bool    `json:"fictional"`
}

type demoDoc struct {
	Fictional        bool            `json:"fictional"`
	Sample           bool            `json:"sample"`
	Banner           string          `json:"banner"`
	NetworkCalls     int             `json:"network_calls"`
	EstimatedTotal   float64         `json:"estimated_total"`
	UnpricedCount    int             `json:"unpriced_count,omitempty"`
	ActualTotal      float64         `json:"actual_total"`
	ActualAvailable  bool            `json:"actual_available"`
	ActualDisplay    string          `json:"actual_display"`
	Warnings         []string        `json:"warnings,omitempty"`
	Resources        []resourceRow   `json:"resources"`
	Snapshots        []cost.Snapshot `json:"snapshots,omitempty"`
	Recommendations  []demoRec       `json:"recommendations"`
	PotentialSavings float64         `json:"potential_savings"`
	SavingsNote      string          `json:"savings_note"`
}

func demoDocument(res cost.Resolution, result optimizer.Result) demoDoc {
	recs := make([]demoRec, 0, len(result.Recommendations))
	for _, rec := range result.Recommendations {
		recs = append(recs, demoRec{
			Action:        string(rec.Action),
			Provider:      rec.Resource.Provider,
			Type:          string(rec.Resource.Type),
			ID:            rec.Resource.ID,
			Name:          rec.Resource.DisplayName(),
			Savings:       rec.Savings,
			SuggestedSpec: rec.SuggestedSpec,
			Reason:        rec.Reason,
			Fictional:     true,
		})
	}
	return demoDoc{
		Fictional:        true,
		Sample:           true,
		Banner:           demo.Banner,
		NetworkCalls:     0,
		EstimatedTotal:   res.EstimatedTotal,
		UnpricedCount:    res.UnpricedCount,
		ActualTotal:      res.ActualTotal,
		ActualAvailable:  res.ActualAvailable,
		ActualDisplay:    actualDisplay(res.ActualAvailable, res.ActualTotal),
		Warnings:         res.Warnings,
		Resources:        rowsFromResources(res.Resources),
		Snapshots:        res.Snapshots,
		Recommendations:  recs,
		PotentialSavings: result.PotentialSavings,
		SavingsNote:      demoSavingsNote,
	}
}

func actualDisplay(available bool, total float64) string {
	if !available {
		return "unavailable"
	}
	return fmt.Sprintf("$%.2f", total)
}

func writeDemoTable(cmd *cobra.Command, res cost.Resolution, result optimizer.Result) error {
	out := cmd.OutOrStdout()
	fmt.Fprintln(out, demo.Banner)
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Inventory (rip-style) and spend (cost-style) on the fictional sample:")
	fmt.Fprint(out, cost.Summary(res))
	rows := rowsFromResources(res.Resources)
	fmt.Fprintf(out, "Found %d fictional resources:\n\n", len(rows))
	headers, table := resourceTable(rows)
	if err := writeFormattedTable(out, headers, table); err != nil {
		return err
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Recommendations (optimize-style) on the fictional sample:")
	fmt.Fprintln(out, demoSavingsNote)
	fmt.Fprint(out, optimizer.FormatReport(result))
	fmt.Fprintln(out, "Recommendations only. The demo does not apply changes.")
	return nil
}

func demoCSV(res cost.Resolution, result optimizer.Result) ([]string, [][]string) {
	headers := []string{"SECTION", "SAMPLE", "FIELD", "PROVIDER", "TYPE", "ID", "NAME", "REGION", "STATUS", "SPEC", "USD", "SOURCE", "ACTION", "SAVINGS", "REASON"}
	var rows [][]string
	rows = append(rows, demoCSVTotals("estimated_total", fmt.Sprintf("%.2f", res.EstimatedTotal))...)
	rows = append(rows, demoCSVTotals("actual_total", actualDisplay(res.ActualAvailable, res.ActualTotal))...)
	rows = append(rows, demoCSVTotals("unpriced_count", fmt.Sprintf("%d", res.UnpricedCount))...)
	_, table := resourceTable(rowsFromResources(res.Resources))
	for _, row := range table {
		rows = append(rows, []string{
			"inventory", "fictional", "",
			row[0], row[1], row[2], row[3], row[4], row[5], row[6], row[7], row[8],
			"", "", "",
		})
	}
	for _, rec := range result.Recommendations {
		rows = append(rows, []string{
			"recommendation", "fictional", "",
			rec.Resource.Provider, string(rec.Resource.Type), rec.Resource.ID, rec.Resource.DisplayName(),
			rec.Resource.Region, rec.Resource.Status, rec.SuggestedSpec, "", "",
			string(rec.Action), fmt.Sprintf("%.2f", rec.Savings), rec.Reason,
		})
	}
	return headers, rows
}

func nonEmptyLines(text string) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func demoCSVTotals(field, value string) [][]string {
	return [][]string{{
		"totals", "fictional", field,
		"", "", "", "", "", "", "", value, "",
		"", "", "",
	}}
}

func demoHTML(res cost.Resolution, result optimizer.Result) output.HTMLReport {
	_, table := resourceTable(rowsFromResources(res.Resources))
	recHeaders, recRows := recommendationTable(result.Recommendations)
	notes := []string{
		demoSavingsNote,
		"Per-resource cost is n/a when the sample marks the row unpriced. That is not a $0 estimate.",
		"Account actuals below are fictional sample totals. A live run with no billing data would say unavailable instead of $0.",
	}
	notes = append(notes, nonEmptyLines(cost.Summary(res))...)
	notes = append(notes, "Actual display: "+actualDisplay(res.ActualAvailable, res.ActualTotal))
	return output.HTMLReport{
		Title:  "CloudRipper fictional sample",
		Banner: demo.Banner,
		Notes:  notes,
		Sections: []output.HTMLSection{
			{
				Heading: "Inventory and spend",
				Headers: []string{"PROVIDER", "TYPE", "ID", "NAME", "REGION", "STATUS", "SPEC", "USD", "SOURCE"},
				Rows:    table,
			},
			{
				Heading: "Recommendations",
				Notes:   []string{fmt.Sprintf("Optimizer monthly total on the fictional rows: $%.2f. Illustrative potential savings: $%.2f. Not real savings.", result.TotalMonthlyCost, result.PotentialSavings)},
				Headers: recHeaders,
				Rows:    recRows,
			},
		},
	}
}
