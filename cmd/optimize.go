package cmd

import (
	"fmt"
	"time"

	"github.com/AndrewGrayYouNeeK/cloudripper/internal/apply"
	"github.com/AndrewGrayYouNeeK/cloudripper/internal/cloud"
	"github.com/AndrewGrayYouNeeK/cloudripper/internal/config"
	"github.com/AndrewGrayYouNeeK/cloudripper/internal/iac"
	"github.com/AndrewGrayYouNeeK/cloudripper/internal/optimizer"
	"github.com/AndrewGrayYouNeeK/cloudripper/internal/output"
	"github.com/spf13/cobra"
)

var optimizeFlags struct {
	apply    bool
	forceIaC bool
	state    string
	planDir  string
}

var optimizeCmd = &cobra.Command{
	Use:   "optimize",
	Short: "Recommend cost cuts and optionally apply them",
	Long: `Scans configured clouds and recommends rightsizing, off-hours schedules,
and deletion of idle disks, addresses, and load balancers.

Without --apply the command only prints the report. --apply stops, starts,
terminates, resizes, or tags resources after one confirmation. --dry-run with
--apply prints the same actions and does not call cloud APIs. Resources tagged
cloudripper:protect=true are skipped. Terraform- or Pulumi-managed resources
get a plan snippet instead of a live change unless --force-iac is set.

Database deletion and Kubernetes cluster deletion are refused.`,
	RunE: runOptimize,
}

func init() {
	optimizeCmd.Flags().BoolVar(&optimizeFlags.apply, "apply", false, "execute recommendations after confirmation")
	optimizeCmd.Flags().BoolVar(&optimizeFlags.forceIaC, "force-iac", false, "mutate IaC-managed resources anyway (state will drift)")
	optimizeCmd.Flags().StringVar(&optimizeFlags.state, "iac-state", "", "Terraform state file, Pulumi export, or directory of both")
	optimizeCmd.Flags().StringVar(&optimizeFlags.planDir, "plan-dir", "", "directory for IaC plan snippets (default .cloudripper/plans)")
	rootCmd.AddCommand(optimizeCmd)
}

func runOptimize(cmd *cobra.Command, _ []string) error {
	cfg, res, err := loadResolution(cmd)
	if err != nil {
		return err
	}
	result := optimizer.Analyze(res.Resources)
	if !optimizeFlags.apply {
		return printAnalysis(cmd, result, cfg.DryRun)
	}
	recs := optimizer.WithImmediateSchedule(result.Recommendations, time.Now().UTC())
	records, err := executeRecs(cmd, cfg, recs, "optimize", optimizeFlags.forceIaC, optimizeFlags.state, optimizeFlags.planDir)
	if err != nil {
		return err
	}
	return printApply(cmd, result, records)
}

func printAnalysis(cmd *cobra.Command, result optimizer.Result, dryRun bool) error {
	format, err := outputFormat()
	if err != nil {
		return err
	}
	if format == output.FormatTable {
		fmt.Fprint(cmd.OutOrStdout(), optimizer.FormatReport(result))
		fmt.Fprintln(cmd.OutOrStdout(), "Recommendations only. Pass --apply to execute them.")
		if dryRun {
			fmt.Fprintln(cmd.OutOrStdout(), "(dry-run mode — no changes applied)")
		}
		return nil
	}
	headers, rows := recommendationTable(result.Recommendations)
	return render(cmd, headers, rows, result)
}

func printApply(cmd *cobra.Command, result optimizer.Result, records []apply.Record) error {
	format, err := outputFormat()
	if err != nil {
		return err
	}
	payload := struct {
		Monthly          float64                    `json:"monthly_cost"`
		PotentialSavings float64                    `json:"potential_savings"`
		Recommendations  []optimizer.Recommendation `json:"recommendations"`
		Records          []apply.Record             `json:"records"`
	}{
		Monthly:          result.TotalMonthlyCost,
		PotentialSavings: result.PotentialSavings,
		Recommendations:  result.Recommendations,
		Records:          records,
	}
	if format == output.FormatTable {
		fmt.Fprint(cmd.OutOrStdout(), optimizer.FormatReport(result))
		fmt.Fprintln(cmd.OutOrStdout(), "Apply results:")
	}
	headers, rows := recordTable(records)
	return render(cmd, headers, rows, payload)
}

func executeRecs(cmd *cobra.Command, cfg config.Config, recs []optimizer.Recommendation, command string, force bool, state, planDir string) ([]apply.Record, error) {
	ctx := commandContext(cmd)
	var execs []cloud.Executor
	if !cfg.DryRun {
		var err error
		execs, err = rt.executors(ctx, cfg)
		if err != nil {
			return nil, err
		}
	}
	var confirmer apply.Confirmer
	if !cfg.DryRun {
		c, err := newConfirmer(cmd)
		if err != nil {
			return nil, err
		}
		confirmer = c
	}
	index, err := iac.Load(state)
	if err != nil {
		return nil, err
	}
	return apply.Execute(ctx, recs, execs, apply.Options{
		DryRun:    cfg.DryRun,
		ForceIaC:  force,
		Confirmer: confirmer,
		Audit:     auditLogger(cfg),
		Index:     index,
		PlanDir:   planDir,
		Command:   command,
	})
}

func recommendationTable(recs []optimizer.Recommendation) ([]string, [][]string) {
	headers := []string{"ACTION", "PROVIDER", "TYPE", "ID", "SAVINGS", "SPEC", "REASON"}
	rows := make([][]string, 0, len(recs))
	for _, rec := range recs {
		rows = append(rows, []string{
			string(rec.Action),
			rec.Resource.Provider,
			string(rec.Resource.Type),
			rec.Resource.ID,
			fmt.Sprintf("%.2f", rec.Savings),
			rec.SuggestedSpec,
			rec.Reason,
		})
	}
	return headers, rows
}

func recordTable(records []apply.Record) ([]string, [][]string) {
	headers := []string{"OUTCOME", "ACTION", "PROVIDER", "ID", "SAVINGS", "DETAIL"}
	rows := make([][]string, 0, len(records))
	for _, rec := range records {
		rows = append(rows, []string{
			rec.Outcome,
			rec.Action,
			rec.Provider,
			rec.ID,
			fmt.Sprintf("%.2f", rec.Savings),
			rec.Detail,
		})
	}
	return headers, rows
}
