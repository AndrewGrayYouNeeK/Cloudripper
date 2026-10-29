package cmd

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/AndrewGrayYouNeeK/cloudripper/internal/apply"
	"github.com/AndrewGrayYouNeeK/cloudripper/internal/cloud"
	"github.com/AndrewGrayYouNeeK/cloudripper/internal/config"
	"github.com/AndrewGrayYouNeeK/cloudripper/internal/cost"
	"github.com/AndrewGrayYouNeeK/cloudripper/internal/notify"
	"github.com/AndrewGrayYouNeeK/cloudripper/internal/optimizer"
	"github.com/AndrewGrayYouNeeK/cloudripper/internal/watch"
	"github.com/spf13/cobra"
)

var watchFlags struct {
	interval  time.Duration
	threshold float64
	once      bool
	state     string
	enforce   bool
}

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Rescan on an interval and alert on cost spikes",
	Long: `Collects a cost sample immediately and then on every interval. The first
sample establishes a baseline and never alerts. Later samples alert when
total or per-cloud spend rises by the threshold versus the previous sample.

Alerts go to a Slack incoming webhook (SLACK_WEBHOOK_URL) and SMTP email
(SMTP_HOST, ALERT_EMAIL_FROM, ALERT_EMAIL_TO). With neither configured, the
alert is printed to stdout.

--enforce-schedules stops and starts instances whose cloudripper:schedule
tag is due. Live enforcement requires --yes. --dry-run records the actions
without calling cloud APIs.`,
	RunE: runWatch,
}

func init() {
	watchCmd.Flags().DurationVar(&watchFlags.interval, "interval", 0, "rescan interval (default WATCH_INTERVAL or 15m)")
	watchCmd.Flags().Float64Var(&watchFlags.threshold, "threshold", 0, "spike ratio (0.25) or percent above 1 (25). 0 uses the default")
	watchCmd.Flags().BoolVar(&watchFlags.once, "once", false, "take a single sample and exit")
	watchCmd.Flags().StringVar(&watchFlags.state, "state", "", "file that keeps recent samples across restarts")
	watchCmd.Flags().BoolVar(&watchFlags.enforce, "enforce-schedules", false, "apply due cloudripper:schedule tags each sample")
	rootCmd.AddCommand(watchCmd)
}

type memoryCollector struct {
	cfg       config.Config
	err       io.Writer
	resources []cloud.Resource
}

func (m *memoryCollector) Collect(ctx context.Context) (watch.Snapshot, error) {
	snap, resources, err := collectWithResources(ctx, m.cfg)
	m.resources = resources
	if err != nil && len(resources) > 0 {
		writeScanProblems(m.err, err)
		return snap, nil
	}
	return snap, err
}

func runWatch(cmd *cobra.Command, _ []string) error {
	cfg := loadConfig()
	if cmd.Flags().Changed("interval") {
		cfg.WatchInterval = watchFlags.interval
	}
	if cmd.Flags().Changed("threshold") {
		cfg.WatchThreshold = watchFlags.threshold
	}
	if watchFlags.enforce && !cfg.DryRun && !yes {
		return fmt.Errorf("refusing to enforce schedules without --yes (or set --dry-run to preview)")
	}
	writeSkips(cmd.ErrOrStderr(), cfg)
	collector := &memoryCollector{cfg: cfg, err: cmd.ErrOrStderr()}
	opts := watch.Options{
		Interval:  cfg.WatchInterval,
		Threshold: cfg.WatchThreshold,
		StatePath: watchFlags.state,
		Notifiers: buildNotifiers(cfg, cmd.OutOrStdout()),
		Collector: collector,
		Output:    cmd.OutOrStdout(),
	}
	if watchFlags.enforce {
		opts.OnTick = func(ctx context.Context, _ watch.Snapshot) error {
			recs := optimizer.PlanSchedules(collector.resources, time.Now().UTC())
			if len(recs) == 0 {
				return nil
			}
			var execs []cloud.Executor
			if !cfg.DryRun {
				var err error
				execs, err = rt.executors(ctx, cfg)
				if err != nil {
					return err
				}
			}
			records, err := apply.Execute(ctx, recs, execs, apply.Options{
				DryRun:    cfg.DryRun,
				Confirmer: batchConfirmer{yes: true},
				Audit:     auditLogger(cfg),
				Command:   "watch",
			})
			if err != nil {
				return err
			}
			for _, rec := range records {
				fmt.Fprintf(cmd.OutOrStdout(), "schedule %s %s %s\n", rec.Outcome, rec.Action, rec.ID)
			}
			return nil
		}
	}
	watcher, err := watch.New(opts)
	if err != nil {
		return err
	}
	ctx := commandContext(cmd)
	if watchFlags.once {
		_, err = watcher.Tick(ctx)
		return err
	}
	return watcher.Run(ctx)
}

func buildNotifiers(cfg config.Config, w io.Writer) []notify.Notifier {
	var out []notify.Notifier
	if cfg.SlackWebhook != "" {
		out = append(out, notify.Slack{Webhook: cfg.SlackWebhook})
	}
	if cfg.SMTPHost != "" && cfg.AlertEmailTo != "" && cfg.AlertEmailFrom != "" {
		out = append(out, notify.Email{
			Host:     cfg.SMTPHost,
			Port:     cfg.SMTPPort,
			Username: cfg.SMTPUser,
			Password: cfg.SMTPPassword,
			From:     cfg.AlertEmailFrom,
			To:       cfg.AlertEmailTo,
		})
	}
	if len(out) == 0 {
		out = append(out, notify.Stdout{Writer: w})
	}
	return out
}

func collectWithResources(ctx context.Context, cfg config.Config) (watch.Snapshot, []cloud.Resource, error) {
	resources, scanErr := rt.scan(ctx, cfg)
	if scanErr != nil && len(resources) == 0 {
		return watch.Snapshot{}, nil, scanErr
	}
	resolution, err := rt.costs(ctx, cfg, resources)
	if err != nil {
		resolution = cost.Resolution{Resources: resources, EstimatedTotal: sumCost(resources)}
	}
	snap := snapshotFrom(resolution)
	return snap, resolution.Resources, scanErr
}

func snapshotFrom(resolution cost.Resolution) watch.Snapshot {
	snap := watch.Snapshot{ByProvider: map[string]float64{}}
	if resolution.ActualAvailable {
		snap.Total = resolution.ActualTotal
		for _, item := range resolution.Snapshots {
			switch item.Source {
			case cloud.CostSourceAWS:
				snap.ByProvider["aws"] += item.TotalUSD
			case cloud.CostSourceGCP:
				snap.ByProvider["gcp"] += item.TotalUSD
			case cloud.CostSourceAzure:
				snap.ByProvider["azure"] += item.TotalUSD
			default:
				snap.ByProvider[item.Source] += item.TotalUSD
			}
		}
		return snap
	}
	for _, r := range resolution.Resources {
		snap.Total += r.CostUSD
		snap.ByProvider[r.Provider] += r.CostUSD
	}
	return snap
}
