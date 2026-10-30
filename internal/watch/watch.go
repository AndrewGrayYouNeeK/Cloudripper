package watch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/notify"
)

// Snapshot is one cost sample.
type Snapshot struct {
	At         time.Time          `json:"at"`
	Total      float64            `json:"total"`
	ByProvider map[string]float64 `json:"by_provider,omitempty"`
}

// Collector produces a cost sample.
type Collector interface {
	Collect(ctx context.Context) (Snapshot, error)
}

// Alert is a detected cost spike.
type Alert struct {
	At       time.Time
	Scope    string
	Current  float64
	Baseline float64
	Delta    float64
	Message  string
}

// State persists recent samples so a restarted daemon still has a baseline.
type State struct {
	Samples []Snapshot `json:"samples"`
}

// Options controls one watcher.
type Options struct {
	Interval   time.Duration
	Threshold  float64
	StatePath  string
	Notifiers  []notify.Notifier
	Collector  Collector
	Output     io.Writer
	Now        func() time.Time
	OnTick     func(ctx context.Context, snap Snapshot) error
	MaxSamples int
}

// Watcher rescans on an interval and alerts on cost spikes.
type Watcher struct {
	Options Options
	state   State
}

// New constructs a watcher and loads prior samples when a state path is set.
func New(opts Options) (*Watcher, error) {
	if opts.Interval <= 0 {
		opts.Interval = 15 * time.Minute
	}
	opts.Threshold = NormalizeThreshold(opts.Threshold)
	if opts.MaxSamples <= 0 {
		opts.MaxSamples = 48
	}
	if opts.Now == nil {
		opts.Now = func() time.Time { return time.Now().UTC() }
	}
	if opts.Output == nil {
		opts.Output = io.Discard
	}
	w := &Watcher{Options: opts}
	if opts.StatePath != "" {
		if err := w.load(); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	return w, nil
}

// NormalizeThreshold accepts either a ratio (0.25) or a percent (25).
func NormalizeThreshold(v float64) float64 {
	if v == 0 {
		return 0.25
	}
	if v > 1 {
		return v / 100
	}
	return v
}

// Tick collects one sample, persists it, and sends alerts.
func (w *Watcher) Tick(ctx context.Context) ([]Alert, error) {
	if w.Options.Collector == nil {
		return nil, fmt.Errorf("watch collector is not configured")
	}
	snap, err := w.Options.Collector.Collect(ctx)
	if err != nil {
		return nil, err
	}
	if snap.At.IsZero() {
		snap.At = w.Options.Now()
	}
	alerts := Detect(w.state.Samples, snap, w.Options.Threshold)
	w.state.Samples = append(w.state.Samples, snap)
	if len(w.state.Samples) > w.Options.MaxSamples {
		w.state.Samples = w.state.Samples[len(w.state.Samples)-w.Options.MaxSamples:]
	}
	if err := w.save(); err != nil {
		return alerts, err
	}
	fmt.Fprintf(w.Options.Output, "%s scan total=$%.2f\n", snap.At.UTC().Format(time.RFC3339), snap.Total)
	for _, alert := range alerts {
		fmt.Fprintln(w.Options.Output, alert.Message)
		msg := notify.Message{Subject: "CloudRipper cost spike", Body: alert.Message}
		if err := notify.Fanout(ctx, w.Options.Notifiers, msg); err != nil {
			return alerts, err
		}
	}
	if w.Options.OnTick != nil {
		if err := w.Options.OnTick(ctx, snap); err != nil {
			return alerts, err
		}
	}
	return alerts, nil
}

// Run scans immediately and then on each interval until ctx is cancelled.
func (w *Watcher) Run(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := w.Tick(ctx); err != nil {
		return err
	}
	ticker := time.NewTicker(w.Options.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if _, err := w.Tick(ctx); err != nil {
				fmt.Fprintf(w.Options.Output, "watch error: %v\n", err)
			}
		}
	}
}

// Detect compares the new sample with the previous total and each provider.
// The first sample never alerts. A sustained high value alerts once, because
// the next comparison uses the previous (already high) sample.
func Detect(history []Snapshot, current Snapshot, threshold float64) []Alert {
	if len(history) == 0 {
		return nil
	}
	prev := history[len(history)-1]
	var alerts []Alert
	if alert, ok := spike("total", prev.Total, current.Total, threshold, current.At); ok {
		alerts = append(alerts, alert)
	}
	for provider, value := range current.ByProvider {
		base := prev.ByProvider[provider]
		if alert, ok := spike(provider, base, value, threshold, current.At); ok {
			alerts = append(alerts, alert)
		}
	}
	return alerts
}

func spike(scope string, baseline, current, threshold float64, at time.Time) (Alert, bool) {
	if baseline <= 0 {
		if current <= 0 {
			return Alert{}, false
		}
		return Alert{}, false
	}
	delta := (current - baseline) / baseline
	if delta < threshold {
		return Alert{}, false
	}
	return Alert{
		At:       at,
		Scope:    scope,
		Current:  current,
		Baseline: baseline,
		Delta:    delta,
		Message: fmt.Sprintf("ALERT %s cost spike +%.0f%% ($%.2f vs $%.2f)",
			scope, delta*100, current, baseline),
	}, true
}

func (w *Watcher) load() error {
	data, err := os.ReadFile(w.Options.StatePath)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &w.state)
}

func (w *Watcher) save() error {
	if w.Options.StatePath == "" {
		return nil
	}
	data, err := json.MarshalIndent(w.state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(w.Options.StatePath, data, 0o644)
}
