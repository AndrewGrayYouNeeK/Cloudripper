package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/pro"
	"github.com/spf13/cobra"
)

var uploadFlags struct {
	account  string
	file     string
	kind     string
	endpoint string
	every    time.Duration
}

// minUploadInterval is the shortest --every value. Tests lower it.
var minUploadInterval = 15 * time.Minute

var uploadCmd = &cobra.Command{
	Use:   "upload",
	Short: "Send a scan report to CloudRipper Pro (optional, opt-in)",
	Long: `Uploads a JSON report to your CloudRipper Pro dashboard. Your cloud
credentials stay on this machine: the scan runs here and only the report JSON
is sent.

Without --file, upload runs the same scan as "rip --output json" and sends it.
With --file, it sends a report you saved earlier from rip, cost, or optimize
with --output json ("-" reads stdin).

--every keeps running on this machine and scans and uploads on that interval
until you stop it. This is the local scheduler for Pro scheduled scans: if the
machine is off or asleep, that run is skipped.

Set CLOUDRIPPER_PRO_URL to your Pro URL (or pass --endpoint) and
CLOUDRIPPER_PRO_TOKEN to an API token from the Pro dashboard. The token is
read only from the environment so it does not end up in shell history.`,
	Example: `  cloudripper upload --account prod-aws
  cloudripper upload --account prod-aws --every 6h
  cloudripper cost --output json > cost.json && cloudripper upload --account prod-aws --file cost.json --kind cost`,
	Args: cobra.NoArgs,
	RunE: runUpload,
}

func init() {
	uploadCmd.Flags().StringVar(&uploadFlags.account, "account", "", "Pro account label for this report, e.g. prod-aws (required)")
	uploadCmd.Flags().StringVar(&uploadFlags.file, "file", "", "upload this saved --output json report instead of scanning (\"-\" for stdin)")
	uploadCmd.Flags().StringVar(&uploadFlags.kind, "kind", "", "report kind for --file: rip, cost, or optimize (default: inferred; rip and cost look the same)")
	uploadCmd.Flags().StringVar(&uploadFlags.endpoint, "endpoint", "", "Pro URL (default: "+pro.EnvURL+")")
	uploadCmd.Flags().DurationVar(&uploadFlags.every, "every", 0, "keep scanning and uploading on this interval, e.g. 6h (minimum 15m)")
	rootCmd.AddCommand(uploadCmd)
}

func runUpload(cmd *cobra.Command, _ []string) error {
	account := strings.TrimSpace(uploadFlags.account)
	if account == "" {
		return errors.New("--account is required (a label such as prod-aws; each label is a separate account in Pro)")
	}
	if uploadFlags.kind != "" && uploadFlags.file == "" {
		return errors.New("--kind only applies with --file; a scan upload is always a rip report")
	}
	switch uploadFlags.kind {
	case "", "rip", "cost", "optimize":
	default:
		return fmt.Errorf("--kind must be rip, cost, or optimize, not %q", uploadFlags.kind)
	}
	if uploadFlags.every != 0 {
		if uploadFlags.file != "" {
			return errors.New("--every rescans each time, so it cannot be combined with --file")
		}
		if uploadFlags.every < minUploadInterval {
			return fmt.Errorf("--every must be at least %s", minUploadInterval)
		}
	}
	endpoint := uploadFlags.endpoint
	if endpoint == "" {
		endpoint = os.Getenv(pro.EnvURL)
	}
	client, err := pro.NewClient(endpoint, os.Getenv(pro.EnvToken))
	if err != nil {
		return err
	}

	if uploadFlags.file != "" {
		up, err := readUploadFile(cmd, account)
		if err != nil {
			return err
		}
		return sendUpload(commandContext(cmd), cmd, client, up)
	}

	once := func(ctx context.Context) error {
		up, err := scanUpload(cmd, account)
		if err != nil {
			return err
		}
		return sendUpload(ctx, cmd, client, up)
	}
	if uploadFlags.every == 0 {
		return once(commandContext(cmd))
	}
	return runEvery(commandContext(cmd), cmd.ErrOrStderr(), uploadFlags.every, once)
}

func readUploadFile(cmd *cobra.Command, account string) (pro.Upload, error) {
	var r io.Reader
	if uploadFlags.file == "-" {
		r = cmd.InOrStdin()
	} else {
		f, err := os.Open(uploadFlags.file)
		if err != nil {
			return pro.Upload{}, err
		}
		defer f.Close()
		r = f
	}
	raw, err := io.ReadAll(io.LimitReader(r, pro.MaxReportBytes+1))
	if err != nil {
		return pro.Upload{}, err
	}
	if len(raw) > pro.MaxReportBytes {
		return pro.Upload{}, fmt.Errorf("report is larger than the %d-byte Pro upload limit", pro.MaxReportBytes)
	}
	kind := uploadFlags.kind
	inferred, err := pro.InferKind(raw)
	if err != nil {
		return pro.Upload{}, err
	}
	if kind == "" {
		kind = inferred
	} else if (kind == "optimize") != (inferred == "optimize") {
		return pro.Upload{}, fmt.Errorf("--kind %s does not match the report, which looks like %s output", kind, inferred)
	}
	return pro.Upload{Account: account, Kind: kind, Report: json.RawMessage(raw)}, nil
}

func scanUpload(cmd *cobra.Command, account string) (pro.Upload, error) {
	cfg, res, err := loadResolution(cmd)
	if err != nil {
		return pro.Upload{}, err
	}
	raw, err := json.Marshal(scanPayload(cfg, res))
	if err != nil {
		return pro.Upload{}, err
	}
	return pro.Upload{
		Account:     account,
		Kind:        "rip",
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Report:      json.RawMessage(raw),
	}, nil
}

func sendUpload(ctx context.Context, cmd *cobra.Command, client *pro.Client, up pro.Upload) error {
	stored, err := client.Upload(ctx, up)
	if err != nil {
		return err
	}
	detail := ""
	switch {
	case stored.ResourceCount != nil:
		detail = fmt.Sprintf(", %d resources", *stored.ResourceCount)
	case stored.RecommendationCount != nil:
		detail = fmt.Sprintf(", %d recommendations", *stored.RecommendationCount)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Uploaded %s report %s to account %q%s.\n", stored.Kind, stored.ID, stored.Account.Label, detail)
	return nil
}

// runEvery calls once now and then every interval until ctx ends. A permanent
// server rejection (bad token, inactive subscription, invalid report) stops
// the loop; other failures are reported and retried on the next tick.
func runEvery(ctx context.Context, stderr io.Writer, interval time.Duration, once func(context.Context) error) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := once(ctx); err != nil {
			if pro.IsPermanent(err) {
				return err
			}
			if ctx.Err() != nil {
				return nil
			}
			fmt.Fprintf(stderr, "warning: %v (will retry in %s)\n", err, interval)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
