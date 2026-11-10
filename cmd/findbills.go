package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/bills"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/demo"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/output"
	appruntime "github.com/AndrewGrayYouNeeK/Cloudripper/internal/runtime"
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
	tokenPath := findBillsFlags.token
	if tokenPath == "" {
		tokenPath = os.Getenv("CLOUDRIPPER_GMAIL_TOKEN")
	}
	result, err := appruntime.FindBills(commandContext(cmd), cfg, appruntime.BillRequest{
		CSVPath:      findBillsFlags.csv,
		Sample:       findBillsFlags.sample,
		GmailToken:   tokenPath,
		Since:        findBillsFlags.since,
		SinceChanged: cmd.Flags().Changed("since"),
	})
	if err != nil {
		return err
	}
	findings := result.Findings

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
	if format == output.FormatHTML {
		return output.WriteHTML(cmd.OutOrStdout(), output.HTMLReport{
			Title: "CloudRipper find-bills",
			Notes: []string{fmt.Sprintf("Found %d cloud bill(s).", len(findings))},
			Sections: []output.HTMLSection{{
				Heading: "Bills",
				Headers: billHeaders(),
				Rows:    table,
			}},
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
		return writeFormattedTable(cmd.OutOrStdout(), headers, table)
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
