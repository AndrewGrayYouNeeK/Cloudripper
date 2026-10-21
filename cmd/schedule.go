package cmd

import (
	"fmt"
	"time"

	"github.com/AndrewGrayYouNeeK/cloudripper/internal/optimizer"
	"github.com/AndrewGrayYouNeeK/cloudripper/internal/output"
	"github.com/spf13/cobra"
)

var scheduleFlags struct {
	apply    bool
	forceIaC bool
	state    string
	planDir  string
}

var scheduleCmd = &cobra.Command{
	Use:   "schedule",
	Short: "Stop or start compute that carries a cloudripper:schedule tag",
	Long: `Reads cloudripper:schedule tags (off=utc:HH-HH) and stops running compute
inside the off window or starts stopped compute outside it.

This is a pull-based check. It does not create EventBridge rules or Cloud
Scheduler jobs. Run it from cron, or pass --enforce-schedules to watch.
Without --apply the command only prints the actions.`,
	RunE: runSchedule,
}

func init() {
	scheduleCmd.Flags().BoolVar(&scheduleFlags.apply, "apply", false, "stop or start matching instances after confirmation")
	scheduleCmd.Flags().BoolVar(&scheduleFlags.forceIaC, "force-iac", false, "mutate IaC-managed resources anyway")
	scheduleCmd.Flags().StringVar(&scheduleFlags.state, "iac-state", "", "Terraform state file, Pulumi export, or directory of both")
	scheduleCmd.Flags().StringVar(&scheduleFlags.planDir, "plan-dir", "", "directory for IaC plan snippets")
	rootCmd.AddCommand(scheduleCmd)
}

func runSchedule(cmd *cobra.Command, _ []string) error {
	cfg, res, err := loadResolution(cmd)
	if err != nil {
		return err
	}
	recs := optimizer.PlanSchedules(res.Resources, time.Now().UTC())
	if recs == nil {
		recs = []optimizer.Recommendation{}
	}
	result := optimizer.Result{Recommendations: recs}
	for _, rec := range recs {
		result.TotalMonthlyCost += rec.Resource.CostUSD
		result.PotentialSavings += rec.Savings
	}
	if !scheduleFlags.apply {
		format, err := outputFormat()
		if err != nil {
			return err
		}
		if format == output.FormatTable {
			if len(recs) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No schedule actions are due.")
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "%d schedule action(s) are due. Pass --apply to execute them.\n", len(recs))
			}
		}
		headers, rows := recommendationTable(recs)
		if format == output.FormatTable && len(recs) == 0 {
			return nil
		}
		return render(cmd, headers, rows, recs)
	}
	records, err := executeRecs(cmd, cfg, recs, "schedule", scheduleFlags.forceIaC, scheduleFlags.state, scheduleFlags.planDir)
	if err != nil {
		return err
	}
	return printApply(cmd, result, records)
}
