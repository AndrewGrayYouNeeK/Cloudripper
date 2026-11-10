package runtime

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/bills"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/cloud"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/config"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/demo"
)

// BillRequest selects the read-only sources for find-bills.
// Sample reads the embedded fictional card export and ignores mail and CSV paths.
type BillRequest struct {
	CSVPath      string
	Sample       bool
	GmailToken   string
	Since        time.Duration
	SinceChanged bool
	Now          time.Time
}

// BillResult is the findings for one find-bills run.
type BillResult struct {
	Findings []bills.Finding
	Sample   bool
}

// FindBills reads the embedded sample, a card CSV, Gmail, or IMAP.
// Mail access stays read-only. Passwords and tokens are not part of the result.
func FindBills(ctx context.Context, cfg config.Config, req BillRequest) (BillResult, error) {
	sinceWindow := 120 * 24 * time.Hour
	if req.SinceChanged {
		sinceWindow = req.Since
	}
	if sinceWindow <= 0 {
		return BillResult{}, fmt.Errorf("since must be positive")
	}
	now := req.Now
	if now.IsZero() {
		now = time.Now()
	}
	since := now.Add(-sinceWindow)

	var messages []bills.Message
	var txns []bills.Transaction
	used := false

	if req.Sample {
		if req.CSVPath != "" {
			return BillResult{}, fmt.Errorf("--sample reads the embedded card export; do not also pass --csv")
		}
		used = true
		reader, err := demo.CardExportReader()
		if err != nil {
			return BillResult{}, err
		}
		parsed, err := bills.ParseCSV(reader)
		if err != nil {
			return BillResult{}, err
		}
		txns = parsed
	} else if req.CSVPath != "" {
		used = true
		file, err := os.Open(req.CSVPath)
		if err != nil {
			return BillResult{}, err
		}
		defer file.Close()
		parsed, err := bills.ParseCSV(file)
		if err != nil {
			return BillResult{}, err
		}
		txns = parsed
	}

	if req.GmailToken != "" && !req.Sample {
		used = true
		gmail := bills.Gmail{TokenPath: req.GmailToken, Since: since}
		got, err := gmail.Messages(ctx)
		if err != nil {
			return BillResult{}, err
		}
		messages = append(messages, got...)
	}

	if host := strings.TrimSpace(os.Getenv("CLOUDRIPPER_IMAP_HOST")); host != "" && !req.Sample {
		used = true
		imap := bills.IMAP{
			Host:     host,
			Port:     os.Getenv("CLOUDRIPPER_IMAP_PORT"),
			User:     os.Getenv("CLOUDRIPPER_IMAP_USER"),
			Password: os.Getenv("CLOUDRIPPER_IMAP_PASSWORD"),
			Mailbox:  os.Getenv("CLOUDRIPPER_IMAP_MAILBOX"),
		}
		got, err := imap.Messages(ctx, since)
		if err != nil {
			return BillResult{}, err
		}
		messages = append(messages, got...)
	}

	if !used {
		return BillResult{}, fmt.Errorf("no bill source: set CLOUDRIPPER_IMAP_HOST, pass --gmail-token, pass --csv, or pass --sample")
	}

	findings := bills.Report(messages, txns, bills.Accounts{
		AWS:        true,
		GCP:        strings.TrimSpace(cfg.GCPProject) != "",
		Azure:      strings.TrimSpace(cfg.AzureSubscription) != "",
		Cloudflare: cloud.CloudflareConfigured(cfg.CloudflareAccountID, cfg.CloudflareAPIToken),
	}, since)
	return BillResult{Findings: findings, Sample: req.Sample}, nil
}
