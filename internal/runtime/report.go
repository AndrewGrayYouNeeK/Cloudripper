package runtime

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/bills"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/cloud"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/config"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/cost"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/demo"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/optimizer"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/output"
)

// DemoSavingsNote is printed with optimizer output on the fictional sample.
const DemoSavingsNote = "Potential savings are the optimizer's heuristic on this fictional inventory. They are not real savings, and the demo does not change any cloud resource."

// LiveSavingsNote describes optimizer output on a real inventory.
const LiveSavingsNote = "Potential savings are the optimizer's heuristics on this inventory. They are not measured billing reductions. This app does not apply changes."

const readOnlyNote = "Read-only. Nothing in this view changes cloud resources."

// ResourceView is one inventory row safe to render and export.
type ResourceView struct {
	Provider    string   `json:"provider"`
	Type        string   `json:"type"`
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Region      string   `json:"region"`
	Status      string   `json:"status"`
	Spec        string   `json:"spec,omitempty"`
	CostUSD     *float64 `json:"cost_usd"`
	CostDisplay string   `json:"cost_display"`
	CostSource  string   `json:"cost_source,omitempty"`
}

// RecommendationView is one optimizer suggestion. It is not an action that was run.
type RecommendationView struct {
	Action         string  `json:"action"`
	Provider       string  `json:"provider"`
	Type           string  `json:"type"`
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	Savings        float64 `json:"savings"`
	SavingsDisplay string  `json:"savings_display"`
	SuggestedSpec  string  `json:"suggested_spec,omitempty"`
	Reason         string  `json:"reason"`
	Fictional      bool    `json:"fictional"`
}

// Report is a read-only rip, cost, optimize, or demo view.
type Report struct {
	Screen                  string               `json:"screen"`
	Title                   string               `json:"title"`
	Fictional               bool                 `json:"fictional"`
	ReadOnly                bool                 `json:"read_only"`
	Banner                  string               `json:"banner,omitempty"`
	EstimatedTotal          float64              `json:"estimated_total"`
	EstimatedDisplay        string               `json:"estimated_display"`
	ActualTotal             float64              `json:"actual_total"`
	ActualAvailable         bool                 `json:"actual_available"`
	ActualDisplay           string               `json:"actual_display"`
	UnpricedCount           int                  `json:"unpriced_count"`
	Warnings                []string             `json:"warnings"`
	Skipped                 []string             `json:"skipped"`
	Resources               []ResourceView       `json:"resources"`
	Recommendations         []RecommendationView `json:"recommendations"`
	PotentialSavings        float64              `json:"potential_savings"`
	PotentialSavingsDisplay string               `json:"potential_savings_display"`
	SavingsNote             string               `json:"savings_note,omitempty"`
	EmptyMessage            string               `json:"empty_message,omitempty"`
	NetworkCalls            int                  `json:"network_calls"`
}

// BillRow is one find-bills row.
type BillRow struct {
	Provider   string `json:"provider"`
	ProviderID string `json:"provider_id"`
	Account    string `json:"account,omitempty"`
	Amount     string `json:"amount"`
	Currency   string `json:"currency,omitempty"`
	Date       string `json:"date,omitempty"`
	Configured string `json:"configured"`
	Status     string `json:"status"`
	Source     string `json:"source"`
}

// BillReport is the find-bills view.
type BillReport struct {
	Fictional    bool      `json:"fictional"`
	Sample       bool      `json:"sample"`
	ReadOnly     bool      `json:"read_only"`
	Banner       string    `json:"banner,omitempty"`
	Title        string    `json:"title"`
	NetworkCalls int       `json:"network_calls"`
	Findings     []BillRow `json:"findings"`
	EmptyMessage string    `json:"empty_message,omitempty"`
	Note         string    `json:"note"`
}

// LaterItem describes a CLI capability this desktop version does not run.
type LaterItem struct {
	Name   string `json:"name"`
	Detail string `json:"detail"`
}

// ComingLater lists mutations and chaos tests that are out of scope.
func ComingLater() []LaterItem {
	return []LaterItem{
		{Name: "Apply", Detail: "Coming later. This version does not change cloud resources."},
		{Name: "Scale", Detail: "Coming later. This version does not scale instance groups."},
		{Name: "Schedule", Detail: "Coming later. This version does not stop or start resources on a schedule."},
		{Name: "Chaos", Detail: "Coming later. This version does not run chaos experiments."},
	}
}

// LoadDemoReport builds the fictional rip, cost, and optimize view.
// It does not read credentials and does not make network calls.
func LoadDemoReport() (Report, error) {
	_, res, err := demo.Load()
	if err != nil {
		return Report{}, err
	}
	result := optimizer.Analyze(res.Resources)
	report := reportFrom("demo", "Fictional sample", true, res, &result, config.Config{})
	report.Banner = demo.Banner
	report.SavingsNote = DemoSavingsNote
	report.NetworkCalls = 0
	return report, nil
}

// LiveReport scans configured clouds. allRegions matches the CLI --all-regions flag.
// withRecommendations adds the optimizer report. Nothing is applied.
func LiveReport(ctx context.Context, screen, title string, allRegions, withRecommendations bool) (Report, error) {
	cfg := config.Load()
	cfg.AllRegions = allRegions
	report := Report{
		Screen:   screen,
		Title:    title,
		ReadOnly: true,
		Skipped:  SkippedProviders(cfg),
		Warnings: []string{},
	}
	resources, err := DefaultScan(ctx, cfg)
	if err != nil && len(resources) == 0 {
		report.EmptyMessage = "No resources found."
		return report, fmt.Errorf("scan failed: %s", redactSecrets(err.Error()))
	}
	if err != nil {
		report.Warnings = append(report.Warnings, redactSecrets(err.Error()))
	}
	res, cerr := DefaultCosts(ctx, cfg, resources)
	if cerr != nil {
		report.Warnings = append(report.Warnings, redactSecrets(cerr.Error()))
		res = cost.Resolution{Resources: resources, EstimatedTotal: sumPriced(resources)}
	}
	var recs *optimizer.Result
	if withRecommendations {
		analyzed := optimizer.Analyze(res.Resources)
		recs = &analyzed
	}
	built := reportFrom(screen, title, false, res, recs, cfg)
	built.Warnings = append(report.Warnings, built.Warnings...)
	if len(built.Resources) == 0 {
		built.EmptyMessage = "No resources found in the scanned region(s). Check credentials, or scan every enabled AWS region."
	}
	return built, nil
}

// LoadBillReport runs find-bills for the desktop app.
func LoadBillReport(ctx context.Context, sample bool, csvPath string) (BillReport, error) {
	cfg := config.Load()
	result, err := FindBills(ctx, cfg, BillRequest{
		CSVPath: csvPath,
		Sample:  sample,
	})
	if err != nil {
		return BillReport{}, err
	}
	return billReportFrom(result), nil
}

func reportFrom(screen, title string, fictional bool, res cost.Resolution, recs *optimizer.Result, cfg config.Config) Report {
	estimated := cost.RoundCents(res.EstimatedTotal)
	actual := cost.RoundCents(res.ActualTotal)
	report := Report{
		Screen:           screen,
		Title:            title,
		Fictional:        fictional,
		ReadOnly:         true,
		EstimatedTotal:   estimated,
		EstimatedDisplay: FormatMoney(estimated),
		ActualTotal:      actual,
		ActualAvailable:  res.ActualAvailable,
		ActualDisplay:    actualDisplay(res.ActualAvailable, actual),
		UnpricedCount:    res.UnpricedCount,
		Warnings:         append([]string{}, res.Warnings...),
		Skipped:          SkippedProviders(cfg),
		Resources:        resourceViews(res.Resources),
		Recommendations:  []RecommendationView{},
	}
	if fictional {
		report.NetworkCalls = 0
		report.SavingsNote = DemoSavingsNote
	}
	if recs != nil {
		savings := cost.RoundCents(recs.PotentialSavings)
		report.PotentialSavings = savings
		report.PotentialSavingsDisplay = FormatMoney(savings)
		report.Recommendations = recommendationViews(recs.Recommendations, fictional)
		if fictional {
			report.SavingsNote = DemoSavingsNote
		} else {
			report.SavingsNote = LiveSavingsNote
		}
	}
	if report.Warnings == nil {
		report.Warnings = []string{}
	}
	if report.Skipped == nil {
		report.Skipped = []string{}
	}
	return report
}

func resourceViews(resources []cloud.Resource) []ResourceView {
	rows := make([]ResourceView, 0, len(resources))
	for _, r := range resources {
		row := ResourceView{
			Provider:   r.Provider,
			Type:       string(r.Type),
			ID:         r.ID,
			Name:       r.DisplayName(),
			Region:     r.Region,
			Status:     r.Status,
			Spec:       r.Spec,
			CostSource: r.CostSource,
		}
		if r.CostPriced() {
			v := cost.RoundCents(r.CostUSD)
			row.CostUSD = &v
			row.CostDisplay = fmt.Sprintf("%.2f", v)
		} else if r.CostSource == "" {
			row.CostDisplay = "0.00"
		} else {
			row.CostDisplay = "n/a"
		}
		rows = append(rows, row)
	}
	return rows
}

func recommendationViews(recs []optimizer.Recommendation, fictional bool) []RecommendationView {
	out := make([]RecommendationView, 0, len(recs))
	for _, rec := range recs {
		savings := cost.RoundCents(rec.Savings)
		out = append(out, RecommendationView{
			Action:         string(rec.Action),
			Provider:       rec.Resource.Provider,
			Type:           string(rec.Resource.Type),
			ID:             rec.Resource.ID,
			Name:           rec.Resource.DisplayName(),
			Savings:        savings,
			SavingsDisplay: fmt.Sprintf("%.2f", savings),
			SuggestedSpec:  rec.SuggestedSpec,
			Reason:         rec.Reason,
			Fictional:      fictional,
		})
	}
	return out
}

func actualDisplay(available bool, total float64) string {
	if !available {
		return "unavailable"
	}
	return FormatMoney(total)
}

func billReportFrom(result BillResult) BillReport {
	rows := make([]BillRow, 0, len(result.Findings))
	for _, finding := range result.Findings {
		rows = append(rows, billRowFrom(finding))
	}
	report := BillReport{
		Fictional: result.Sample,
		Sample:    result.Sample,
		ReadOnly:  true,
		Title:     "Find bills",
		Findings:  rows,
		Note:      "Mail is read-only. Tokens and passwords are not shown.",
	}
	if result.Sample {
		report.Title = "Fictional card sample"
		report.Banner = demo.Banner
		report.NetworkCalls = 0
		report.Note = "Card charges in this report come from the embedded sample export. They are made up."
	}
	if len(rows) == 0 {
		if result.Sample {
			report.EmptyMessage = "No cloud bills found in the fictional sample."
		} else {
			report.EmptyMessage = "No cloud bills found."
		}
	}
	return report
}

func billRowFrom(finding bills.Finding) BillRow {
	date := ""
	if !finding.Date.IsZero() {
		date = finding.Date.UTC().Format("2006-01-02")
	}
	configured := "false"
	if finding.Configured {
		configured = "true"
	}
	return BillRow{
		Provider:   finding.Provider,
		ProviderID: finding.ProviderID,
		Account:    finding.Account,
		Amount:     fmt.Sprintf("%.2f", finding.Amount),
		Currency:   finding.Currency,
		Date:       date,
		Configured: configured,
		Status:     finding.Status,
		Source:     finding.Source,
	}
}

func sumPriced(resources []cloud.Resource) float64 {
	var total float64
	for _, r := range resources {
		if !r.CostPriced() {
			continue
		}
		total += r.CostUSD
	}
	return total
}

// ExportReport writes HTML, CSV, or JSON through internal/output.
func ExportReport(format string, report Report) ([]byte, error) {
	headers, rows := resourceTable(report)
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "json":
		return writeJSON(report)
	case "csv":
		return writeCSV(headers, rows)
	case "html":
		notes := reportNotes(report)
		sections := []output.HTMLSection{{
			Heading: "Resources",
			Headers: headers,
			Rows:    rows,
		}}
		if len(report.Recommendations) > 0 {
			recHeaders, recRows := recommendationTable(report)
			sections = append(sections, output.HTMLSection{
				Heading: "Recommendations",
				Notes:   []string{report.SavingsNote},
				Headers: recHeaders,
				Rows:    recRows,
			})
		}
		return writeHTML(output.HTMLReport{
			Title:    report.Title,
			Banner:   report.Banner,
			Notes:    notes,
			Sections: sections,
		})
	default:
		return nil, fmt.Errorf("unknown export format %q (want html, csv, or json)", format)
	}
}

// ExportBills writes a find-bills report through internal/output.
func ExportBills(format string, report BillReport) ([]byte, error) {
	headers := []string{"PROVIDER", "ACCOUNT", "AMOUNT", "DATE", "CONFIGURED", "STATUS", "SOURCE"}
	if report.Sample {
		headers = append([]string{"SAMPLE"}, headers...)
	}
	rows := make([][]string, 0, len(report.Findings))
	for _, row := range report.Findings {
		record := []string{row.Provider, row.Account, row.Amount, row.Date, row.Configured, row.Status, row.Source}
		if report.Sample {
			record = append([]string{"fictional"}, record...)
		}
		rows = append(rows, record)
	}
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "json":
		return writeJSON(report)
	case "csv":
		return writeCSV(headers, rows)
	case "html":
		notes := []string{report.Note}
		if report.EmptyMessage != "" {
			notes = append(notes, report.EmptyMessage)
		}
		return writeHTML(output.HTMLReport{
			Title:  report.Title,
			Banner: report.Banner,
			Notes:  notes,
			Sections: []output.HTMLSection{{
				Heading: "Bills",
				Headers: headers,
				Rows:    rows,
			}},
		})
	default:
		return nil, fmt.Errorf("unknown export format %q (want html, csv, or json)", format)
	}
}

func reportNotes(report Report) []string {
	notes := []string{
		readOnlyNote,
		"Estimated resource total: " + report.EstimatedDisplay,
		"Actual spend: " + report.ActualDisplay,
		"Unpriced per-resource costs are shown as n/a, not as $0. When billing data was not returned, actual spend is unavailable, not $0.",
	}
	if report.UnpricedCount > 0 {
		notes = append(notes, fmt.Sprintf("%d resource row(s) are unpriced and excluded from the estimated total.", report.UnpricedCount))
	}
	if report.SavingsNote != "" {
		notes = append(notes, report.SavingsNote)
	}
	notes = append(notes, report.Warnings...)
	notes = append(notes, report.Skipped...)
	if report.EmptyMessage != "" {
		notes = append(notes, report.EmptyMessage)
	}
	return notes
}

func resourceTable(report Report) ([]string, [][]string) {
	headers := []string{"PROVIDER", "TYPE", "ID", "NAME", "REGION", "STATUS", "SPEC", "USD", "SOURCE"}
	rows := make([][]string, 0, len(report.Resources))
	for _, row := range report.Resources {
		rows = append(rows, []string{
			row.Provider, row.Type, row.ID, row.Name, row.Region, row.Status, row.Spec, row.CostDisplay, row.CostSource,
		})
	}
	return headers, rows
}

func recommendationTable(report Report) ([]string, [][]string) {
	headers := []string{"ACTION", "PROVIDER", "TYPE", "ID", "SAVINGS", "SPEC", "REASON"}
	rows := make([][]string, 0, len(report.Recommendations))
	for _, rec := range report.Recommendations {
		rows = append(rows, []string{
			rec.Action, rec.Provider, rec.Type, rec.ID, rec.SavingsDisplay, rec.SuggestedSpec, rec.Reason,
		})
	}
	return headers, rows
}

func writeJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := output.WriteJSON(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeCSV(headers []string, rows [][]string) ([]byte, error) {
	var buf bytes.Buffer
	if err := output.WriteCSV(&buf, headers, rows); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeHTML(report output.HTMLReport) ([]byte, error) {
	var buf bytes.Buffer
	if err := output.WriteHTML(&buf, report); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
