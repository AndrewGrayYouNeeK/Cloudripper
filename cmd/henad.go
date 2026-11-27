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

	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/henad"
	appruntime "github.com/AndrewGrayYouNeeK/Cloudripper/internal/runtime"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var henadFlags struct {
	iUnderstand bool
	model       string
	demo        bool
	watchState  string
	threshold   float64
	json        bool
}

// henadStdinIsTerminal is replaced in tests. It asks the OS whether stdin is
// a real terminal (a character device such as /dev/null is not).
var henadStdinIsTerminal = func(r io.Reader) bool {
	f, ok := r.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// henadNow is replaced in tests.
var henadNow = time.Now

var henadCmd = &cobra.Command{
	Use:     "henad",
	Aliases: []string{"nous"}, // Henad was called Nous; the old command still works.
	Short:   "Optional offline AI insights from Henad (local Ollama only, off by default)",
	Long: `Henad explains cost spikes, suggests savings, and answers questions about
your inventory. ` + henad.Slogan + `

Henad runs only on the Ollama model on this computer (` + henad.DefaultEndpoint + `).
CloudRipper refuses any Ollama address that is not localhost, ignores proxies,
and sends nothing off this machine.

Henad is OFF until you turn it on with "cloudripper henad enable", which shows a
warning and asks you to type "I understand". The global --yes flag does not
turn it on; scripts must pass --i-understand. "cloudripper henad disable" turns
it off again.`,
	Example: `  cloudripper henad enable
  cloudripper henad ask "Why is my AWS bill so high?" --demo
  cloudripper henad savings
  cloudripper henad spikes --watch-state ~/.cloudripper-watch.json
  cloudripper henad disable`,
	Args: cobra.NoArgs,
	RunE: runHenadStatus,
}

var henadEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: `Turn Henad on (shows a warning; you must type "I understand")`,
	Args:  cobra.NoArgs,
	RunE:  runHenadEnable,
}

var henadDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Turn Henad off",
	Args:  cobra.NoArgs,
	RunE:  runHenadDisable,
}

var henadStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show whether Henad is on and which local model it would use",
	Args:  cobra.NoArgs,
	RunE:  runHenadStatus,
}

var henadAskCmd = &cobra.Command{
	Use:   "ask <question>",
	Short: "Ask Henad about your inventory and costs",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runHenadTask(cmd, henad.TaskAsk, strings.Join(args, " "))
	},
}

var henadSavingsCmd = &cobra.Command{
	Use:   "savings [question]",
	Short: "Ask Henad where you can save money",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runHenadTask(cmd, henad.TaskSavings, strings.Join(args, " "))
	},
}

var henadSpikesCmd = &cobra.Command{
	Use:   "spikes [question]",
	Short: "Ask Henad to explain cost spikes from cloudripper watch samples",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runHenadTask(cmd, henad.TaskSpikes, strings.Join(args, " "))
	},
}

func init() {
	henadEnableCmd.Flags().BoolVar(&henadFlags.iUnderstand, "i-understand", false, `turn Henad on without the prompt (non-interactive). You confirm you read "cloudripper henad enable" warning`)
	henadEnableCmd.Flags().StringVar(&henadFlags.model, "model", "", "Ollama model to use (default: the first installed chat model)")
	for _, c := range []*cobra.Command{henadAskCmd, henadSavingsCmd, henadSpikesCmd} {
		c.Flags().BoolVar(&henadFlags.demo, "demo", false, "use the fictional demo sample instead of scanning your clouds")
		c.Flags().StringVar(&henadFlags.watchState, "watch-state", "", "cost-watch state file from \"cloudripper watch --state\" (for spikes)")
		c.Flags().Float64Var(&henadFlags.threshold, "threshold", 25, "spike threshold percent for the watch samples")
		c.Flags().StringVar(&henadFlags.model, "model", "", "Ollama model for this question")
		c.Flags().BoolVar(&henadFlags.json, "json", false, "print the answer as JSON")
	}
	henadCmd.AddCommand(henadEnableCmd, henadDisableCmd, henadStatusCmd, henadAskCmd, henadSavingsCmd, henadSpikesCmd)
	rootCmd.AddCommand(henadCmd)
}

func writeHenadWarning(w io.Writer) {
	fmt.Fprintf(w, "\n  ⚠  %s\n\n", henad.WarningTitle)
	for _, line := range henad.WarningLines() {
		fmt.Fprintf(w, "  • %s\n", line)
	}
	fmt.Fprintln(w)
}

func runHenadEnable(cmd *cobra.Command, _ []string) error {
	path, err := henad.DefaultPath()
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	writeHenadWarning(out)
	phrase := ""
	switch {
	case henadFlags.iUnderstand:
		phrase = henad.ConfirmPhrase
		fmt.Fprintln(out, "  --i-understand was passed.")
	case !henadStdinIsTerminal(cmd.InOrStdin()):
		return errors.New(`refusing to turn on Henad without confirmation: run "cloudripper henad enable" in a terminal and type "I understand", or pass --i-understand`)
	default:
		fmt.Fprintf(out, "  Type %q to turn on Henad (anything else cancels): ", henad.ConfirmPhrase)
		line, rerr := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
		if rerr != nil && !errors.Is(rerr, io.EOF) {
			return rerr
		}
		phrase = line
	}
	settings, err := henad.Enable(path, phrase, henadFlags.model, henadNow())
	if errors.Is(err, henad.ErrNotConfirmed) {
		fmt.Fprintln(out, "\n  Cancelled. Henad stays off.")
		return nil
	}
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "\n  Henad is on. It uses local Ollama only; nothing leaves this machine.")
	if settings.Model != "" {
		fmt.Fprintf(out, "  Model: %s\n", settings.Model)
	}
	fmt.Fprintf(out, "  Settings: %s\n  Turn it off: cloudripper henad disable\n", path)
	return nil
}

func runHenadDisable(cmd *cobra.Command, _ []string) error {
	path, err := henad.DefaultPath()
	if err != nil {
		return err
	}
	if err := henad.Disable(path); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Henad is off.")
	return nil
}

func runHenadStatus(cmd *cobra.Command, _ []string) error {
	path, err := henad.DefaultPath()
	if err != nil {
		return err
	}
	settings, err := henad.Load(path)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Henad · %s\n", henad.Slogan)
	if !settings.Enabled {
		fmt.Fprintln(out, "Status: OFF (default). Turn it on: cloudripper henad enable")
		return nil
	}
	fmt.Fprintf(out, "Status: ON since %s. Local Ollama only; nothing leaves this machine.\n", settings.EnabledAt)
	endpoint := henad.Endpoint()
	client, err := henad.NewClient(endpoint)
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
	model := henad.PickModel(models, settings.Model)
	if model == "" {
		fmt.Fprintf(out, "Ollama: running at %s, %v\n", endpoint, henad.ErrNoModel)
		return nil
	}
	fmt.Fprintf(out, "Ollama: running at %s. Model: %s\n", endpoint, model)
	return nil
}

// henadReport is replaced in tests.
var henadReport = func(ctx context.Context, demoOnly bool) (appruntime.Report, error) {
	if demoOnly {
		return appruntime.LoadDemoReport()
	}
	return appruntime.LiveReport(ctx, "henad", "Inventory and cost", allRegions, true)
}

func runHenadTask(cmd *cobra.Command, task henad.Task, question string) error {
	path, err := henad.DefaultPath()
	if err != nil {
		return err
	}
	settings, err := henad.Load(path)
	if err != nil {
		return err
	}
	// Check the opt-in before scanning anything.
	if !settings.Enabled {
		return henad.ErrDisabled
	}
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	report, err := henadReport(ctx, henadFlags.demo)
	if err != nil {
		return err
	}
	samples, err := henad.LoadSamples(henadFlags.watchState)
	if err != nil {
		return err
	}
	answer, err := henad.Ask(ctx, henad.Request{
		Settings:  settings,
		Model:     henadFlags.model,
		Task:      task,
		Question:  question,
		Report:    report,
		Samples:   samples,
		Threshold: henadFlags.threshold,
	})
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if henadFlags.json {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(answer)
	}
	fmt.Fprintf(out, "Henad (offline · %s):\n%s\n", answer.Model, answer.Text)
	if report.Fictional {
		fmt.Fprintln(out, "\n(Answer based on the fictional demo sample.)")
	}
	return nil
}
