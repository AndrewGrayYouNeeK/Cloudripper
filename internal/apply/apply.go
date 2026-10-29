package apply

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/audit"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/cloud"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/iac"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/optimizer"
)

const (
	OutcomeApplied     = "applied"
	OutcomeDryRun      = "dry-run"
	OutcomeProtected   = "skipped-protected"
	OutcomeIaC         = "skipped-iac"
	OutcomeConfirm     = "skipped-confirm"
	OutcomeUnsupported = "skipped-unsupported"
	OutcomeFailed      = "failed"
)

// Confirmer gates a batch of mutations.
type Confirmer interface {
	Confirm(summary string) (bool, error)
}

// YesConfirmer accepts the batch. Use it for --yes.
type YesConfirmer struct{}

// Confirm implements Confirmer.
func (YesConfirmer) Confirm(string) (bool, error) { return true, nil }

// NoConfirmer rejects the batch.
type NoConfirmer struct{}

// Confirm implements Confirmer.
func (NoConfirmer) Confirm(string) (bool, error) { return false, nil }

// StdinConfirmer prompts on a terminal.
type StdinConfirmer struct {
	In  io.Reader
	Out io.Writer
}

// Confirm implements Confirmer.
func (c StdinConfirmer) Confirm(summary string) (bool, error) {
	if c.Out == nil {
		c.Out = io.Discard
	}
	fmt.Fprintln(c.Out, summary)
	fmt.Fprint(c.Out, "Apply these changes? [y/N]: ")
	if c.In == nil {
		return false, fmt.Errorf("no input available for confirmation")
	}
	line, err := bufio.NewReader(c.In).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes", nil
}

// Options controls one apply run.
type Options struct {
	DryRun    bool
	ForceIaC  bool
	Confirmer Confirmer
	Audit     *audit.Logger
	Index     *iac.Index
	PlanDir   string
	Command   string
}

// Record is the outcome for one recommendation.
type Record struct {
	Provider string  `json:"provider"`
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Action   string  `json:"action"`
	Outcome  string  `json:"outcome"`
	Detail   string  `json:"detail,omitempty"`
	Savings  float64 `json:"savings"`
}

type planned struct {
	rec        optimizer.Recommendation
	outcome    string
	detail     string
	executable bool
}

// Execute classifies recommendations, confirms once, and calls executors.
// Dry-run never calls an executor. Protected and IaC-managed resources are
// skipped unless ForceIaC is set for the latter.
func Execute(ctx context.Context, recs []optimizer.Recommendation, execs []cloud.Executor, opts Options) ([]Record, error) {
	plans := make([]planned, 0, len(recs))
	var executable int
	for _, rec := range recs {
		item := planned{rec: rec}
		if cloud.IsProtected(rec.Resource.Tags) {
			item.outcome = OutcomeProtected
			item.detail = "tag " + cloud.ProtectTag + " is set"
		} else if info, ok := opts.Index.Lookup(rec.Resource.ID, rec.Resource.Tags); ok && !opts.ForceIaC {
			path, err := iac.WritePlan(opts.PlanDir, rec.Resource, info, string(rec.Action), rec.SuggestedSpec)
			item.outcome = OutcomeIaC
			if err != nil {
				item.detail = info.Tool + " managed; plan write failed: " + err.Error()
			} else {
				item.detail = fmt.Sprintf("%s managed (%s); wrote %s", info.Tool, info.Source, path)
			}
		} else {
			item.executable = true
			executable++
			if info, managed := opts.Index.Lookup(rec.Resource.ID, rec.Resource.Tags); managed && opts.ForceIaC {
				item.detail = "forcing change to " + info.Tool + "-managed resource"
			}
		}
		plans = append(plans, item)
	}

	if opts.DryRun {
		for i := range plans {
			if plans[i].executable {
				plans[i].outcome = OutcomeDryRun
				if plans[i].detail == "" {
					plans[i].detail = "dry-run"
				}
			}
		}
		return finish(plans, opts), nil
	}

	if executable == 0 {
		return finish(plans, opts), nil
	}
	if opts.Confirmer == nil {
		return nil, fmt.Errorf("refusing to apply without confirmation (--yes or an interactive terminal)")
	}
	ok, err := opts.Confirmer.Confirm(summary(plans))
	if err != nil {
		return nil, err
	}
	if !ok {
		for i := range plans {
			if plans[i].executable {
				plans[i].outcome = OutcomeConfirm
				plans[i].detail = "confirmation declined"
			}
		}
		return finish(plans, opts), nil
	}

	for i := range plans {
		if !plans[i].executable {
			continue
		}
		rec := plans[i].rec
		exec := findExecutor(execs, rec.Resource.Provider)
		if exec == nil {
			plans[i].outcome = OutcomeFailed
			plans[i].detail = "no executor for provider " + rec.Resource.Provider
			continue
		}
		detail, err := exec.Execute(ctx, rec.Resource, string(rec.Action), rec.SuggestedSpec)
		if err != nil {
			var unsupported *cloud.UnsupportedError
			if errors.As(err, &unsupported) {
				plans[i].outcome = OutcomeUnsupported
				plans[i].detail = err.Error()
				continue
			}
			plans[i].outcome = OutcomeFailed
			plans[i].detail = err.Error()
			continue
		}
		plans[i].outcome = OutcomeApplied
		if detail != "" {
			plans[i].detail = joinDetail(plans[i].detail, detail)
		}
	}
	return finish(plans, opts), nil
}

func joinDetail(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return a + "; " + b
}

func findExecutor(execs []cloud.Executor, provider string) cloud.Executor {
	for _, exec := range execs {
		if exec != nil && exec.Provider() == provider {
			return exec
		}
	}
	return nil
}

func summary(plans []planned) string {
	var b strings.Builder
	b.WriteString("CloudRipper will apply:\n")
	n := 1
	for _, item := range plans {
		if !item.executable {
			continue
		}
		rec := item.rec
		fmt.Fprintf(&b, "  %d. %s %s/%s (%s)", n, rec.Action, rec.Resource.Provider, rec.Resource.DisplayName(), rec.Resource.ID)
		if rec.SuggestedSpec != "" {
			fmt.Fprintf(&b, " -> %s", rec.SuggestedSpec)
		}
		fmt.Fprintf(&b, " save $%.2f\n", rec.Savings)
		n++
	}
	skipped := 0
	for _, item := range plans {
		if !item.executable {
			skipped++
		}
	}
	if skipped > 0 {
		fmt.Fprintf(&b, "Skipping %d protected or IaC-managed resource(s).\n", skipped)
	}
	return b.String()
}

func finish(plans []planned, opts Options) []Record {
	records := make([]Record, 0, len(plans))
	for _, item := range plans {
		rec := Record{
			Provider: item.rec.Resource.Provider,
			ID:       item.rec.Resource.ID,
			Name:     item.rec.Resource.DisplayName(),
			Action:   string(item.rec.Action),
			Outcome:  item.outcome,
			Detail:   item.detail,
			Savings:  item.rec.Savings,
		}
		records = append(records, rec)
		if opts.Audit != nil {
			_ = opts.Audit.Write(audit.Event{
				Command:      opts.Command,
				Action:       rec.Action,
				Provider:     rec.Provider,
				ResourceID:   rec.ID,
				ResourceName: rec.Name,
				Outcome:      rec.Outcome,
				Detail:       rec.Detail,
				DryRun:       opts.DryRun || rec.Outcome == OutcomeDryRun,
			})
		}
	}
	return records
}
