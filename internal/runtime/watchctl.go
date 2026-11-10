package runtime

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/config"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/cost"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/notify"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/watch"
)

// WatchAlert is one spike the watcher reported. It does not include credentials.
type WatchAlert struct {
	At       string  `json:"at"`
	Scope    string  `json:"scope"`
	Message  string  `json:"message"`
	Current  float64 `json:"current"`
	Baseline float64 `json:"baseline"`
}

// WatchState is the desktop watch panel. Notifier names are labels such as
// "slack" or "os". Webhook URLs and passwords are not included.
type WatchState struct {
	Running          bool         `json:"running"`
	Interval         string       `json:"interval"`
	ThresholdPercent float64      `json:"threshold_percent"`
	SlackConfigured  bool         `json:"slack_configured"`
	EmailConfigured  bool         `json:"email_configured"`
	OSNotifications  bool         `json:"os_notifications"`
	Notifiers        []string     `json:"notifiers"`
	LastTotal        string       `json:"last_total,omitempty"`
	LastAt           string       `json:"last_at,omitempty"`
	Alerts           []WatchAlert `json:"alerts"`
	Log              []string     `json:"log"`
	LastError        string       `json:"last_error,omitempty"`
	Note             string       `json:"note"`
}

// WatchOptions configures a desktop watch loop.
// Collector is optional; when it is nil, samples come from the live clouds.
// Notifiers are invoked by internal/watch. The desktop app adds the OS notifier.
type WatchOptions struct {
	Interval  time.Duration
	Threshold float64
	StatePath string
	Notifiers []notify.Notifier
	Collector watch.Collector
	OnUpdate  func(WatchState)
	Config    config.Config
	ConfigSet bool
}

// WatchController runs internal/watch without schedule enforcement.
type WatchController struct {
	mu      sync.Mutex
	cancel  context.CancelFunc
	running bool
	state   WatchState
}

// NewWatchController returns an idle controller.
func NewWatchController() *WatchController {
	return &WatchController{state: defaultWatchState()}
}

func defaultWatchState() WatchState {
	return WatchState{
		Interval:         "15m0s",
		ThresholdPercent: 25,
		Notifiers:        []string{},
		Alerts:           []WatchAlert{},
		Log:              []string{},
		Note:             "The first sample is a baseline and does not alert. Later samples alert when spend rises by the threshold versus the previous sample. This app does not stop, start, scale, or otherwise change cloud resources.",
	}
}

// State returns a copy of the current watch panel.
func (w *WatchController) State() WatchState {
	w.mu.Lock()
	defer w.mu.Unlock()
	return cloneWatchState(w.state)
}

// Stop ends a running loop. It is safe to call when the loop is idle.
func (w *WatchController) Stop() WatchState {
	w.mu.Lock()
	cancel := w.cancel
	w.cancel = nil
	w.running = false
	w.state.Running = false
	state := cloneWatchState(w.state)
	w.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return state
}

// Once takes a single sample and returns. It does not enforce schedules.
func (w *WatchController) Once(ctx context.Context, opts WatchOptions) (WatchState, error) {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return w.State(), fmt.Errorf("watch is already running")
	}
	w.running = true
	w.state.Running = true
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		w.running = false
		w.state.Running = false
		w.mu.Unlock()
	}()

	watcher, err := w.prepare(opts)
	if err != nil {
		w.setError(err)
		return w.State(), err
	}
	alerts, tickErr := watcher.Tick(ctx)
	w.recordTick(watcher, alerts, tickErr, opts)
	if tickErr != nil {
		return w.State(), tickErr
	}
	return w.State(), nil
}

// Start samples immediately and then on the interval until Stop.
// The loop does not enforce schedules.
func (w *WatchController) Start(ctx context.Context, opts WatchOptions) (WatchState, error) {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return w.State(), fmt.Errorf("watch is already running")
	}
	runCtx, cancel := context.WithCancel(ctx)
	w.cancel = cancel
	w.running = true
	w.state.Running = true
	w.mu.Unlock()

	watcher, err := w.prepare(opts)
	if err != nil {
		w.Stop()
		w.setError(err)
		return w.State(), err
	}
	go w.loop(runCtx, watcher, opts)
	return w.State(), nil
}

func (w *WatchController) loop(ctx context.Context, watcher *watch.Watcher, opts WatchOptions) {
	defer w.Stop()
	alerts, err := watcher.Tick(ctx)
	w.recordTick(watcher, alerts, err, opts)
	if err != nil {
		return
	}
	ticker := time.NewTicker(watcher.Options.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			alerts, err = watcher.Tick(ctx)
			w.recordTick(watcher, alerts, err, opts)
		}
	}
}

func (w *WatchController) prepare(opts WatchOptions) (*watch.Watcher, error) {
	cfg := opts.Config
	if !opts.ConfigSet {
		cfg = config.Load()
	}
	interval := opts.Interval
	if interval <= 0 {
		interval = cfg.WatchInterval
	}
	if interval <= 0 {
		interval = 15 * time.Minute
	}
	threshold := opts.Threshold
	if threshold == 0 {
		threshold = cfg.WatchThreshold
	}
	collector := opts.Collector
	if collector == nil {
		collector = liveCollector(cfg)
	}
	var output bytes.Buffer
	watcher, err := watch.New(watch.Options{
		Interval:  interval,
		Threshold: threshold,
		StatePath: opts.StatePath,
		Notifiers: opts.Notifiers,
		Collector: &recordingCollector{inner: collector},
		Output:    &output,
	})
	if err != nil {
		return nil, err
	}
	slack, email := WatchChannels(cfg)
	w.mu.Lock()
	w.state.Interval = watcher.Options.Interval.String()
	w.state.ThresholdPercent = watcher.Options.Threshold * 100
	w.state.SlackConfigured = slack
	w.state.EmailConfigured = email
	w.state.OSNotifications = hasNotifier(opts.Notifiers, "os")
	w.state.Notifiers = notifierNames(opts.Notifiers)
	w.state.Note = defaultWatchState().Note
	w.mu.Unlock()
	return watcher, nil
}

func (w *WatchController) recordTick(watcher *watch.Watcher, alerts []watch.Alert, tickErr error, opts WatchOptions) {
	output := ""
	if buf, ok := watcher.Options.Output.(*bytes.Buffer); ok {
		output = buf.String()
		buf.Reset()
	}
	w.mu.Lock()
	if output != "" {
		for _, line := range splitLines(output) {
			w.state.Log = append(w.state.Log, redactSecrets(line))
		}
		if len(w.state.Log) > 200 {
			w.state.Log = w.state.Log[len(w.state.Log)-200:]
		}
	}
	if rec, ok := watcher.Options.Collector.(*recordingCollector); ok {
		rec.mu.Lock()
		last := rec.last
		rec.mu.Unlock()
		if !last.At.IsZero() {
			w.state.LastAt = last.At.UTC().Format(time.RFC3339)
			w.state.LastTotal = FormatMoney(cost.RoundCents(last.Total))
		}
	}
	for _, alert := range alerts {
		at := ""
		if !alert.At.IsZero() {
			at = alert.At.UTC().Format(time.RFC3339)
		}
		w.state.Alerts = append(w.state.Alerts, WatchAlert{
			At:       at,
			Scope:    alert.Scope,
			Message:  alert.Message,
			Current:  cost.RoundCents(alert.Current),
			Baseline: cost.RoundCents(alert.Baseline),
		})
	}
	if len(w.state.Alerts) > 100 {
		w.state.Alerts = w.state.Alerts[len(w.state.Alerts)-100:]
	}
	if tickErr != nil {
		w.state.LastError = redactSecrets(tickErr.Error())
	} else {
		w.state.LastError = ""
	}
	state := cloneWatchState(w.state)
	w.mu.Unlock()
	if opts.OnUpdate != nil {
		opts.OnUpdate(state)
	}
}

func (w *WatchController) setError(err error) {
	w.mu.Lock()
	w.state.LastError = redactSecrets(err.Error())
	w.mu.Unlock()
}

type recordingCollector struct {
	inner watch.Collector
	last  watch.Snapshot
	mu    sync.Mutex
}

func (r *recordingCollector) Collect(ctx context.Context) (watch.Snapshot, error) {
	snap, err := r.inner.Collect(ctx)
	if snap.At.IsZero() {
		snap.At = time.Now().UTC()
	}
	r.mu.Lock()
	r.last = snap
	r.mu.Unlock()
	return snap, err
}

func liveCollector(cfg config.Config) watch.Collector {
	return collectorFunc(func(ctx context.Context) (watch.Snapshot, error) {
		resources, scanErr := DefaultScan(ctx, cfg)
		if scanErr != nil && len(resources) == 0 {
			return watch.Snapshot{}, fmt.Errorf("scan failed: %s", redactSecrets(scanErr.Error()))
		}
		resolution, err := DefaultCosts(ctx, cfg, resources)
		if err != nil {
			resolution = cost.Resolution{Resources: resources, EstimatedTotal: sumPriced(resources)}
		}
		snap := SnapshotFrom(resolution)
		if snap.At.IsZero() {
			snap.At = time.Now().UTC()
		}
		return snap, nil
	})
}

type collectorFunc func(context.Context) (watch.Snapshot, error)

func (f collectorFunc) Collect(ctx context.Context) (watch.Snapshot, error) {
	return f(ctx)
}

func notifierNames(notifiers []notify.Notifier) []string {
	names := make([]string, 0, len(notifiers))
	for _, n := range notifiers {
		if n == nil {
			continue
		}
		names = append(names, n.Name())
	}
	return names
}

func hasNotifier(notifiers []notify.Notifier, name string) bool {
	for _, n := range notifierNames(notifiers) {
		if n == name {
			return true
		}
	}
	return false
}

func splitLines(text string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] != '\n' {
			continue
		}
		line := text[start:i]
		start = i + 1
		if line != "" {
			lines = append(lines, line)
		}
	}
	if start < len(text) && text[start:] != "" {
		lines = append(lines, text[start:])
	}
	return lines
}

func cloneWatchState(in WatchState) WatchState {
	out := in
	out.Notifiers = append([]string{}, in.Notifiers...)
	out.Alerts = append([]WatchAlert{}, in.Alerts...)
	out.Log = append([]string{}, in.Log...)
	if out.Notifiers == nil {
		out.Notifiers = []string{}
	}
	if out.Alerts == nil {
		out.Alerts = []WatchAlert{}
	}
	if out.Log == nil {
		out.Log = []string{}
	}
	return out
}
