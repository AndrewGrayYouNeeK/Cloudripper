package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/bills"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/cloud"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/demo"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/output"
	"github.com/spf13/cobra"
)

var findBillsFlags struct {
	csv    string
	token  string
	since  time.Duration
	sample bool
}

var findBillsCmd = &cobra.Command{
	Use:   "find-bills",
	Short: "Find cloud bills in mail or a card export that CloudRipper is not configured for",
	Long: `Reads billing mail and an optional bank or card CSV, then lists each cloud
provider it recognizes, the account id when the message contains one, and the
latest amount and date.

Mail is read-only. IMAP uses EXAMINE and BODY.PEEK, so messages are not marked
read, deleted, or flagged. Gmail uses GET requests against an OAuth token file.
Passwords and tokens are not written to the output or the audit log.

AWS is treated as configured because CloudRipper always scans it. GCP is
configured when GCP_PROJECT is set, Azure when AZURE_SUBSCRIPTION_ID is set,
and Cloudflare when CLOUDFLARE_ACCOUNT_ID and CLOUDFLARE_API_TOKEN are set.
Any other recognized provider is marked possibly forgotten.

IMAP environment: CLOUDRIPPER_IMAP_HOST, CLOUDRIPPER_IMAP_PORT (993),
CLOUDRIPPER_IMAP_USER, CLOUDRIPPER_IMAP_PASSWORD, CLOUDRIPPER_IMAP_MAILBOX (INBOX).
Gmail: --gmail-token or CLOUDRIPPER_GMAIL_TOKEN, a JSON file with access_token.
The token needs the gmail.readonly scope.

--sample reads the fictional card export embedded in the binary (the same
bytes as docs/examples/sample-card-export.csv). It does not open a file,
mail server, or any network connection. The charges are made up.`,
	RunE: runFindBills,
}

func init() {
	findBillsCmd.Flags().StringVar(&findBillsFlags.csv, "csv", "", "bank or card CSV export (date, description, amount)")
	findBillsCmd.Flags().BoolVar(&findBillsFlags.sample, "sample", false, "read the embedded fictional card export; no file and no network")
	findBillsCmd.Flags().StringVar(&findBillsFlags.token, "gmail-token", "", "Gmail OAuth token JSON (or CLOUDRIPPER_GMAIL_TOKEN)")
	findBillsCmd.Flags().DurationVar(&findBillsFlags.since, "since", 0, "how far back to look (default 120 days)")
	rootCmd.AddCommand(findBillsCmd)
}

func runFindBills(cmd *cobra.Command, _ []string) error {
	cfg := loadConfig()
	sinceWindow := 120 * 24 * time.Hour
	if cmd.Flags().Changed("since") {
		sinceWindow = findBillsFlags.since
	}
	if sinceWindow <= 0 {
		return fmt.Errorf("since must be positive")
	}
	since := time.Now().Add(-sinceWindow)
	ctx := commandContext(cmd)

	var messages []bills.Message
	var txns []bills.Transaction
	used := false

	if findBillsFlags.sample {
		if findBillsFlags.csv != "" {
			return fmt.Errorf("--sample reads the embedded card export; do not also pass --csv")
		}
		used = true
		reader, err := demo.CardExportReader()
		if err != nil {
			return err
		}
		parsed, err := bills.ParseCSV(reader)
		if err != nil {
			return err
		}
		txns = parsed
	} else if findBillsFlags.csv != "" {
		used = true
		file, err := os.Open(findBillsFlags.csv)
		if err != nil {
			return err
		}
		defer file.Close()
		parsed, err := bills.ParseCSV(file)
		if err != nil {
			return err
		}
		txns = parsed
	}

	tokenPath := findBillsFlags.token
	if tokenPath == "" {
		tokenPath = os.Getenv("CLOUDRIPPER_GMAIL_TOKEN")
	}
	if tokenPath != "" && !findBillsFlags.sample {
		used = true
		gmail := bills.Gmail{TokenPath: tokenPath, Since: since}
		got, err := gmail.Messages(ctx)
		if err != nil {
			return err
		}
		messages = append(messages, got...)
	}

	if host := strings.TrimSpace(os.Getenv("CLOUDRIPPER_IMAP_HOST")); host != "" && !findBillsFlags.sample {
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
			return err
		}
		messages = append(messages, got...)
	}

	if !used {
		return fmt.Errorf("no bill source: set CLOUDRIPPER_IMAP_HOST, pass --gmail-token, pass --csv, or pass --sample")
	}

	findings := bills.Report(messages, txns, bills.Accounts{
		AWS:        true,
		GCP:        strings.TrimSpace(cfg.GCPProject) != "",
		Azure:      strings.TrimSpace(cfg.AzureSubscription) != "",
		Cloudflare: cloud.CloudflareConfigured(cfg.CloudflareAccountID, cfg.CloudflareAPIToken),
	}, since)

	format, err := outputFormat()
	if err != nil {
		return err
	}
	if findBillsFlags.sample {
		return renderSampleBills(cmd, format, findings)
	}
	if len(findings) == 0 {
		if format == output.FormatTable {
			fmt.Fprintln(cmd.OutOrStdout(), "No cloud bills found.")
		}
		return render(cmd, billHeaders(), nil, []billRow{})
	}
	if format == output.FormatTable {
		fmt.Fprintf(cmd.OutOrStdout(), "Found %d cloud bill(s):\n\n", len(findings))
	}
	rows := make([]billRow, 0, len(findings))
	table := make([][]string, 0, len(findings))
	for _, finding := range findings {
		row := billRowFrom(finding)
		rows = append(rows, row)
		table = append(table, []string{
			row.Provider, row.Account, row.Amount, row.Date, row.Configured, row.Status, row.Source,
		})
	}
	return render(cmd, billHeaders(), table, rows)
}

func billHeaders() []string {
	return []string{"PROVIDER", "ACCOUNT", "AMOUNT", "DATE", "CONFIGURED", "STATUS", "SOURCE"}
}

type billRow struct {
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

func renderSampleBills(cmd *cobra.Command, format output.Format, findings []bills.Finding) error {
	rows := make([]billRow, 0, len(findings))
	table := make([][]string, 0, len(findings))
	for _, finding := range findings {
		row := billRowFrom(finding)
		rows = append(rows, row)
		table = append(table, []string{
			"fictional", row.Provider, row.Account, row.Amount, row.Date, row.Configured, row.Status, row.Source,
		})
	}
	headers := append([]string{"SAMPLE"}, billHeaders()...)
	switch format {
	case output.FormatJSON:
		fmt.Fprintln(cmd.ErrOrStderr(), demo.Banner)
		return output.WriteJSON(cmd.OutOrStdout(), struct {
			Fictional    bool      `json:"fictional"`
			Sample       bool      `json:"sample"`
			Banner       string    `json:"banner"`
			NetworkCalls int       `json:"network_calls"`
			Findings     []billRow `json:"findings"`
		}{
			Fictional:    true,
			Sample:       true,
			Banner:       demo.Banner,
			NetworkCalls: 0,
			Findings:     rows,
		})
	case output.FormatCSV:
		fmt.Fprintln(cmd.ErrOrStderr(), demo.Banner)
		return output.WriteCSV(cmd.OutOrStdout(), headers, table)
	case output.FormatHTML:
		notes := []string{"Card charges in this report come from the embedded sample export. They are made up."}
		if len(findings) == 0 {
			notes = append(notes, "No cloud bills in the sample window.")
		}
		return output.WriteHTML(cmd.OutOrStdout(), output.HTMLReport{
			Title:    "CloudRipper fictional card sample",
			Banner:   demo.Banner,
			Notes:    notes,
			Sections: []output.HTMLSection{{Heading: "Card export", Headers: headers, Rows: table}},
		})
	case output.FormatTable:
		fmt.Fprintln(cmd.OutOrStdout(), demo.Banner)
		fmt.Fprintln(cmd.OutOrStdout())
		if len(findings) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "No cloud bills found in the fictional sample.")
			return nil
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Found %d fictional cloud bill(s):\n\n", len(findings))
		return output.WriteTable(cmd.OutOrStdout(), headers, table)
	default:
		return fmt.Errorf("unhandled output format %q", format)
	}
}

func billRowFrom(finding bills.Finding) billRow {
	date := ""
	if !finding.Date.IsZero() {
		date = finding.Date.UTC().Format("2006-01-02")
	}
	configured := "false"
	if finding.Configured {
		configured = "true"
	}
	return billRow{
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
