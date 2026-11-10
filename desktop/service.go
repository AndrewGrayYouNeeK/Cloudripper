//go:build desktop

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/config"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/notify"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/runtime"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

// AppService is the read-only API the desktop UI calls.
// It has no apply, scale, schedule, or chaos methods.
type AppService struct {
	app           *application.App
	notifications *notifications.NotificationService
	watch         *runtime.WatchController
}

// NewAppService builds the desktop API. The Wails app is attached before run.
func NewAppService(notes *notifications.NotificationService) *AppService {
	return &AppService{
		notifications: notes,
		watch:         runtime.NewWatchController(),
	}
}

// Attach stores the application used for dialogs and watch events.
//
//wails:ignore
func (s *AppService) Attach(app *application.App) {
	s.app = app
}

// ServiceStartup requests OS notification permission. Failure is not fatal;
// alerts still appear inside the window.
func (s *AppService) ServiceStartup(context.Context, application.ServiceOptions) error {
	if s.app == nil {
		s.app = application.Get()
	}
	if s.notifications != nil {
		_, _ = s.notifications.RequestNotificationAuthorization()
	}
	return nil
}

// ServiceShutdown stops a running watch loop.
func (s *AppService) ServiceShutdown() error {
	if s.watch != nil {
		s.watch.Stop()
	}
	return nil
}

// Demo returns the embedded fictional sample. It does not use credentials or the network.
func (s *AppService) Demo(context.Context) (runtime.Report, error) {
	return runtime.LoadDemoReport()
}

// Providers reports local credential status. Secret values are not returned.
func (s *AppService) Providers(ctx context.Context) ([]runtime.ProviderStatus, error) {
	return runtime.ProviderStatuses(ctx, config.Load()), nil
}

// Inventory returns a rip view. live false uses the fictional sample.
func (s *AppService) Inventory(ctx context.Context, live bool, allRegions bool) (runtime.Report, error) {
	if !live {
		report, err := runtime.LoadDemoReport()
		if err != nil {
			return report, err
		}
		report.Screen = "inventory"
		report.Title = "Fictional inventory"
		return report, nil
	}
	return runtime.LiveReport(ctx, "inventory", "Inventory", allRegions, false)
}

// Spend returns a cost view. live false uses the fictional sample.
func (s *AppService) Spend(ctx context.Context, live bool, allRegions bool) (runtime.Report, error) {
	if !live {
		report, err := runtime.LoadDemoReport()
		if err != nil {
			return report, err
		}
		report.Screen = "spend"
		report.Title = "Fictional spend"
		return report, nil
	}
	return runtime.LiveReport(ctx, "spend", "Cost", allRegions, false)
}

// Recommendations returns optimizer output and does not apply it.
func (s *AppService) Recommendations(ctx context.Context, live bool, allRegions bool) (runtime.Report, error) {
	if !live {
		report, err := runtime.LoadDemoReport()
		if err != nil {
			return report, err
		}
		report.Screen = "recommendations"
		report.Title = "Fictional recommendations"
		return report, nil
	}
	return runtime.LiveReport(ctx, "recommendations", "Recommendations", allRegions, true)
}

// Bills returns find-bills rows. sample true reads the embedded card export.
// csvPath is used only when sample is false.
func (s *AppService) Bills(ctx context.Context, sample bool, csvPath string) (runtime.BillReport, error) {
	report, err := runtime.LoadBillReport(ctx, sample, csvPath)
	if err != nil && strings.Contains(err.Error(), "no bill source") {
		return report, fmt.Errorf("no bill source: set CLOUDRIPPER_IMAP_HOST, set CLOUDRIPPER_GMAIL_TOKEN, or choose a CSV")
	}
	return report, err
}

// ChooseCSV asks for a local card CSV. An empty path means the dialog was cancelled.
func (s *AppService) ChooseCSV(context.Context) (string, error) {
	app := s.application()
	if app == nil {
		return "", fmt.Errorf("file dialog is unavailable")
	}
	path, err := app.Dialog.OpenFile().
		AddFilter("CSV", "*.csv").
		CanChooseFiles(true).
		PromptForSingleSelection()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(path), nil
}

// Export writes the current view as HTML, CSV, or JSON. An empty path means the dialog was cancelled.
func (s *AppService) Export(ctx context.Context, screen string, format string, live bool, allRegions bool, csvPath string) (string, error) {
	data, filename, err := s.exportBytes(ctx, screen, format, live, allRegions, csvPath)
	if err != nil {
		return "", err
	}
	app := s.application()
	if app == nil {
		return "", fmt.Errorf("save dialog is unavailable")
	}
	filterName, pattern := exportFilter(format)
	path, err := app.Dialog.SaveFile().
		SetFilename(filename).
		AddFilter(filterName, pattern).
		PromptForSingleSelection()
	if err != nil {
		return "", err
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return "", nil
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// ComingLater lists actions this version does not perform.
func (s *AppService) ComingLater() []runtime.LaterItem {
	return runtime.ComingLater()
}

// WatchState returns the alert panel. It does not include webhook URLs or passwords.
func (s *AppService) WatchState() runtime.WatchState {
	state := s.watch.State()
	s.decorateWatch(&state)
	return state
}

// WatchStart begins interval sampling. minutes and thresholdPercent use the CLI defaults when they are 0.
func (s *AppService) WatchStart(_ context.Context, minutes float64, thresholdPercent float64) (runtime.WatchState, error) {
	state, err := s.watch.Start(context.Background(), s.watchOptions(minutes, thresholdPercent))
	s.decorateWatch(&state)
	return state, err
}

// WatchOnce takes one sample.
func (s *AppService) WatchOnce(ctx context.Context, minutes float64, thresholdPercent float64) (runtime.WatchState, error) {
	state, err := s.watch.Once(ctx, s.watchOptions(minutes, thresholdPercent))
	s.decorateWatch(&state)
	return state, err
}

// WatchStop ends interval sampling.
func (s *AppService) WatchStop() runtime.WatchState {
	state := s.watch.Stop()
	s.decorateWatch(&state)
	return state
}

func (s *AppService) watchOptions(minutes float64, thresholdPercent float64) runtime.WatchOptions {
	cfg := config.Load()
	interval := cfg.WatchInterval
	if minutes > 0 {
		interval = time.Duration(minutes * float64(time.Minute))
	}
	if interval < time.Minute {
		interval = time.Minute
	}
	threshold := thresholdPercent
	if threshold == 0 {
		threshold = cfg.WatchThreshold
	}
	return runtime.WatchOptions{
		Interval:  interval,
		Threshold: threshold,
		StatePath: watchStatePath(),
		Notifiers: s.notifiers(cfg),
		Config:    cfg,
		ConfigSet: true,
		OnUpdate: func(state runtime.WatchState) {
			s.decorateWatch(&state)
			if s.app != nil {
				s.app.Event.Emit("watch:update", state)
			}
		},
	}
}

func (s *AppService) decorateWatch(state *runtime.WatchState) {
	cfg := config.Load()
	slack, email := runtime.WatchChannels(cfg)
	state.SlackConfigured = slack
	state.EmailConfigured = email
	state.OSNotifications = true
}

func (s *AppService) notifiers(cfg config.Config) []notify.Notifier {
	var out []notify.Notifier
	if strings.TrimSpace(cfg.SlackWebhook) != "" {
		out = append(out, notify.Slack{Webhook: cfg.SlackWebhook})
	}
	if strings.TrimSpace(cfg.SMTPHost) != "" && strings.TrimSpace(cfg.AlertEmailFrom) != "" && strings.TrimSpace(cfg.AlertEmailTo) != "" {
		out = append(out, notify.Email{
			Host:     cfg.SMTPHost,
			Port:     cfg.SMTPPort,
			Username: cfg.SMTPUser,
			Password: cfg.SMTPPassword,
			From:     cfg.AlertEmailFrom,
			To:       cfg.AlertEmailTo,
		})
	}
	if s.notifications != nil {
		out = append(out, &osNotifier{service: s.notifications})
	}
	return out
}

func (s *AppService) exportBytes(ctx context.Context, screen, format string, live, allRegions bool, csvPath string) ([]byte, string, error) {
	ext := strings.ToLower(strings.TrimSpace(format))
	switch ext {
	case "html", "csv", "json":
	default:
		return nil, "", fmt.Errorf("unknown export format %q (want html, csv, or json)", format)
	}
	switch screen {
	case "demo":
		report, err := runtime.LoadDemoReport()
		if err != nil {
			return nil, "", err
		}
		data, err := runtime.ExportReport(ext, report)
		return data, "cloudripper-demo." + ext, err
	case "inventory":
		report, err := s.Inventory(ctx, live, allRegions)
		if err != nil {
			return nil, "", err
		}
		data, err := runtime.ExportReport(ext, report)
		return data, fileName("inventory", live, ext), err
	case "spend":
		report, err := s.Spend(ctx, live, allRegions)
		if err != nil {
			return nil, "", err
		}
		data, err := runtime.ExportReport(ext, report)
		return data, fileName("cost", live, ext), err
	case "recommendations":
		report, err := s.Recommendations(ctx, live, allRegions)
		if err != nil {
			return nil, "", err
		}
		data, err := runtime.ExportReport(ext, report)
		return data, fileName("optimize", live, ext), err
	case "bills":
		report, err := runtime.LoadBillReport(ctx, !live, csvPath)
		if err != nil {
			return nil, "", err
		}
		data, err := runtime.ExportBills(ext, report)
		name := "cloudripper-find-bills." + ext
		if !live {
			name = "cloudripper-find-bills-sample." + ext
		}
		return data, name, err
	default:
		return nil, "", fmt.Errorf("unknown export screen %q", screen)
	}
}

func fileName(kind string, live bool, ext string) string {
	if !live {
		return "cloudripper-" + kind + "-sample." + ext
	}
	return "cloudripper-" + kind + "." + ext
}

func exportFilter(format string) (string, string) {
	switch strings.ToLower(format) {
	case "csv":
		return "CSV", "*.csv"
	case "json":
		return "JSON", "*.json"
	default:
		return "HTML", "*.html"
	}
}

func (s *AppService) application() *application.App {
	if s.app != nil {
		return s.app
	}
	return application.Get()
}

func watchStatePath() string {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "cloudripper")
	_ = os.MkdirAll(dir, 0o700)
	return filepath.Join(dir, "desktop-watch.json")
}
