package cmd

import (
	"fmt"

	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/output"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/scale"
	"github.com/spf13/cobra"
)

var scaleFlags struct {
	policy string
	apply  bool
	state  string
}

var scaleCmd = &cobra.Command{
	Use:   "scale",
	Short: "Scale AWS ASGs and GCP managed instance groups from CPU",
	Long: `Evaluates CPU from CloudWatch or Cloud Monitoring and changes desired
capacity, or resizes a single EC2 or GCE instance when the policy kind is
ec2 or gce.

With no --policy file, CloudRipper discovers Auto Scaling groups and managed
instance groups and applies a default horizontal policy (scale out above 70%
CPU, scale in below 30%, step 1, 5 minute cooldown). EC2 and GCE are not
resized unless a policy names them.

Without --apply the pass is a dry-run. --apply asks for one confirmation
unless --yes is set. --dry-run forces a preview even with --apply.`,
	RunE: runScale,
}

func init() {
	scaleCmd.Flags().StringVar(&scaleFlags.policy, "policy", "", "JSON policy file")
	scaleCmd.Flags().BoolVar(&scaleFlags.apply, "apply", false, "change capacity after confirmation")
	scaleCmd.Flags().StringVar(&scaleFlags.state, "state", "", "cooldown state file (optional)")
	rootCmd.AddCommand(scaleCmd)
}

func runScale(cmd *cobra.Command, _ []string) error {
	cfg := loadConfig()
	ctx := commandContext(cmd)
	controllers, err := rt.scalers(ctx, cfg)
	if err != nil {
		return err
	}
	var policies []scale.Policy
	if scaleFlags.policy != "" {
		policies, err = scale.LoadFile(scaleFlags.policy)
		if err != nil {
			return err
		}
	}
	dry := cfg.DryRun || !scaleFlags.apply
	var confirmer scale.Confirmer
	if !dry {
		c, err := newConfirmer(cmd)
		if err != nil {
			return err
		}
		confirmer = c
	}
	results, err := scale.Run(ctx, controllers, scale.RunOptions{
		Policies:  policies,
		DryRun:    dry,
		Confirmer: confirmer,
		StatePath: scaleFlags.state,
		Audit:     auditLogger(cfg),
		Command:   "scale",
	})
	if err != nil && len(results) == 0 {
		return err
	}
	if err != nil {
		warnf(cmd, "%v", err)
	}
	format, ferr := outputFormat()
	if ferr != nil {
		return ferr
	}
	if format == output.FormatTable && dry {
		fmt.Fprintln(cmd.OutOrStdout(), "Scale preview. Pass --apply to change capacity.")
	}
	headers := []string{"OUTCOME", "ACTION", "PROVIDER", "KIND", "TARGET", "FROM", "TO", "CPU", "DETAIL"}
	rows := make([][]string, 0, len(results))
	for _, result := range results {
		rows = append(rows, []string{
			result.Outcome,
			result.Action,
			result.Provider,
			result.Kind,
			result.Target,
			fmt.Sprintf("%d", result.From),
			fmt.Sprintf("%d", result.To),
			fmt.Sprintf("%.1f", result.CPU),
			result.Detail,
		})
	}
	return render(cmd, headers, rows, results)
}
