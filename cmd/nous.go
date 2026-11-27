package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/nous"
	appruntime "github.com/AndrewGrayYouNeeK/Cloudripper/internal/runtime"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var nousFlags struct {
	iUnderstand bool
	model       string
	demo        bool
	watchState  string
	threshold   float64
	json        bool
}

// nousStdinIsTerminal is replaced in tests. It asks the OS whether stdin is
// a real terminal (a character device such as /dev/null is not).
var nousStdinIsTerminal = func(r io.Reader) bool {
	f, ok := r.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// nousNow is replaced in tests.
var nousNow = time.Now

var nousCmd = &cobra.Command{
	Use:   "nous",
	Short: "Optional offline AI insights from Nous (local Ollama only, off by default)",
	Long: `Nous explains cost spikes, suggests savings, and answers questions about
your inventory. ` + nous.Slogan + `

Nous runs only on the Ollama model on this computer (` + nous.DefaultEndpoint + `).
CloudRipper refuses any Ollama address that is not localhost, ignores proxies,
and sends nothing off this machine.

Nous is OFF until you turn it on with "cloudripper nous enable", which shows a
warning and asks you to type "I understand". The global --yes flag does not
turn it on; scripts must pass --i-understand. "cloudripper nous disable" turns
it off again.`,
	Example: `  cloudripper nous enable
  cloudripper nous ask "Why is my AWS bill so high?" --demo
  cloudripper nous savings
  cloudripper nous spikes --watch-state ~/.cloudripper-watch.json
  cloudripper nous disable`,
	Args: cobra.NoArgs,
	RunE: runNousStatus,
}

var nousEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: `Turn Nous on (shows a warning; you must type "I understand")`,
	Args:  cobra.NoArgs,
	RunE:  runNousEnable,
}

var nousDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Turn Nous off",
	Args:  cobra.NoArgs,
	RunE:  runNousDisable,
}

var nousStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show whether Nous is on and which local model it would use",
	Args:  cobra.NoArgs,
	RunE:  runNousStatus,
}

var nousAskCmd = &cobra.Command{
	Use:   "ask <question>",
	Short: "Ask Nous about your inventory and costs",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runNousTask(cmd, nous.TaskAsk, strings.Join(args, " "))
	},
}

var nousSavingsCmd = &cobra.Command{
	Use:   "savings [question]",
	Short: "Ask Nous where you can save money",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runNousTask(cmd, nous.TaskSavings, strings.Join(args, " "))
	},
}

var nousSpikesCmd = &cobra.Command{
	Use:   "spikes [question]",
	Short: "Ask Nous to explain cost spikes from cloudripper watch samples",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runNousTask(cmd, nous.TaskSpikes, strings.Join(args, " "))
	},
}

func init() {
	nousEnableCmd.Flags().BoolVar(&nousFlags.iUnderstand, "i-understand", false, `turn Nous on without the prompt (non-interactive). You confirm you read "cloudripper nous enable" warning`)
	nousEnableCmd.Flags().StringVar(&nousFlags.model, "model", "", "Ollama model to use (default: the first installed chat model)")
	for _, c := range []*cobra.Command{nousAskCmd, nousSavingsCmd, nousSpikesCmd} {
		c.Flags().BoolVar(&nousFlags.demo, "demo", false, "use the fictional demo sample instead of scanning your clouds")
		c.Flags().StringVar(&nousFlags.watchState, "watch-state", "", "cost-watch state file from \"cloudripper watch --state\" (for spikes)")
		c.Flags().Float64Var(&nousFlags.threshold, "threshold", 25, "spike threshold percent for the watch samples")
		c.Flags().StringVar(&nousFlags.model, "model", "", "Ollama model for this question")
		c.Flags().BoolVar(&nousFlags.json, "json", false, "print the answer as JSON")
	}
	nousCmd.AddCommand(nousEnableCmd, nousDisableCmd, nousStatusCmd, nousAskCmd, nousSavingsCmd, nousSpikesCmd)
	rootCmd.AddCommand(nousCmd)
}

func writeNousWarning(w io.Writer) {
	fmt.Fprintf(w, "\n  ⚠  %s\n\n", nous.WarningTitle)
	for _, line := range nous.WarningLines() {
		fmt.Fprintf(w, "  • %s\n", line)
	}
	fmt.Fprintln(w)
}

func runNousEnable(cmd *cobra.Command, _ []string) error {
	path, err := nous.DefaultPath()
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	writeNousWarning(out)
	phrase := ""
	switch {
	case nousFlags.iUnderstand:
		phrase = nous.ConfirmPhrase
		fmt.Fprintln(out, "  --i-understand was passed.")
	case !nousStdinIsTerminal(cmd.InOrStdin()):
		return errors.New(`refusing to turn on Nous without confirmation: run "cloudripper nous enable" in a terminal and type "I understand", or pass --i-understand`)
	default:
		fmt.Fprintf(out, "  Type %q to turn on Nous (anything else cancels): ", nous.ConfirmPhrase)
		line, rerr := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
		if rerr != nil && !errors.Is(rerr, io.EOF) {
			return rerr
		}
		phrase = line
	}
	settings, err := nous.Enable(path, phrase, nousFlags.model, nousNow())
	if errors.Is(err, nous.ErrNotConfirmed) {
		fmt.Fprintln(out, "\n  Cancelled. Nous stays off.")
		return nil
	}
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "\n  Nous is on. It uses local Ollama only; nothing leaves this machine.")
	if settings.Model != "" {
		fmt.Fprintf(out, "  Model: %s\n", settings.Model)
	}
	fmt.Fprintf(out, "  Settings: %s\n  Turn it off: cloudripper nous disable\n", path)
	return nil
}

func runNousDisable(cmd *cobra.Command, _ []string) error {
	path, err := nous.DefaultPath()
	if err != nil {
		return err
	}
	if err := nous.Disable(path); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Nous is off.")
	return nil
}

func runNousStatus(cmd *cobra.Command, _ []string) error {
	path, err := nous.DefaultPath()
	if err != nil {
		return err
	}
	settings, err := nous.Load(path)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Nous · %s\n", nous.Slogan)
	if !settings.Enabled {
		fmt.Fprintln(out, "Status: OFF (default). Turn it on: cloudripper nous enable")
		return nil
	}
	fmt.Fprintf(out, "Status: ON since %s. Local Ollama only; nothing leaves this machine.\n", settings.EnabledAt)
	endpoint := nous.Endpoint()
	client, err := nous.NewClient(endpoint)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
	defer cancel()
	models, err := client.Models(ctx)
	if err != nil {
		fmt.Fprintf(out, "Ollama: %v\n", err)
		return nil
	}
	model := nous.PickModel(models, settings.Model)
	if model == "" {
		fmt.Fprintf(out, "Ollama: running at %s, %v\n", endpoint, nous.ErrNoModel)
		return nil
	}
	fmt.Fprintf(out, "Ollama: running at %s. Model: %s\n", endpoint, model)
	return nil
}

// nousReport is replaced in tests.
var nousReport = func(ctx context.Context, demoOnly bool) (appruntime.Report, error) {
	if demoOnly {
		return appruntime.LoadDemoReport()
	}
	return appruntime.LiveReport(ctx, "nous", "Inventory and cost", allRegions, true)
}

func runNousTask(cmd *cobra.Command, task nous.Task, question string) error {
	path, err := nous.DefaultPath()
	if err != nil {
		return err
	}
	settings, err := nous.Load(path)
	if err != nil {
		return err
	}
	// Check the opt-in before scanning anything.
	if !settings.Enabled {
		return nous.ErrDisabled
	}
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	report, err := nousReport(ctx, nousFlags.demo)
	if err != nil {
		return err
	}
	samples, err := nous.LoadSamples(nousFlags.watchState)
	if err != nil {
		return err
	}
	answer, err := nous.Ask(ctx, nous.Request{
		Settings:  settings,
		Model:     nousFlags.model,
		Task:      task,
		Question:  question,
		Report:    report,
		Samples:   samples,
		Threshold: nousFlags.threshold,
	})
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if nousFlags.json {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(answer)
	}
	fmt.Fprintf(out, "Nous (offline · %s):\n%s\n", answer.Model, answer.Text)
	if report.Fictional {
		fmt.Fprintln(out, "\n(Answer based on the fictional demo sample.)")
	}
	return nil
}
