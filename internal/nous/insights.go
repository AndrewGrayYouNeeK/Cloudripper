package nous

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	appruntime "github.com/AndrewGrayYouNeeK/Cloudripper/internal/runtime"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/watch"
)

// Task is what the user asked Nous to do.
type Task string

const (
	// TaskAsk answers a free-form question about the report.
	TaskAsk Task = "ask"
	// TaskSavings suggests where to save money.
	TaskSavings Task = "savings"
	// TaskSpikes explains cost spikes from cost-watch samples.
	TaskSpikes Task = "spikes"
)

// ParseTask accepts ask, savings, or spikes. Empty means ask.
func ParseTask(s string) (Task, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "ask":
		return TaskAsk, nil
	case "savings":
		return TaskSavings, nil
	case "spikes", "spike":
		return TaskSpikes, nil
	}
	return "", fmt.Errorf("unknown Nous task %q (ask, savings, or spikes)", s)
}

// maxResources keeps the prompt small enough for small local models.
const maxResources = 40

// BuildContext turns a report and recent cost-watch samples into the text
// the local model reads. This is everything Nous reads.
func BuildContext(r appruntime.Report, samples []watch.Snapshot, threshold float64) string {
	var b strings.Builder
	title := r.Title
	if title == "" {
		title = "CloudRipper report"
	}
	fmt.Fprintf(&b, "Report: %s\n", title)
	if r.Fictional {
		b.WriteString("NOTE: this is CloudRipper's fictional demo sample, not real spend.\n")
	}
	if r.EstimatedDisplay != "" {
		fmt.Fprintf(&b, "Estimated monthly total: %s\n", r.EstimatedDisplay)
	}
	if r.ActualAvailable {
		fmt.Fprintf(&b, "Actual billed total: %s\n", r.ActualDisplay)
	}
	if r.UnpricedCount > 0 {
		fmt.Fprintf(&b, "Resources without a price: %d\n", r.UnpricedCount)
	}
	resources := append([]appruntime.ResourceView(nil), r.Resources...)
	sort.SliceStable(resources, func(i, j int) bool { return cost(resources[i]) > cost(resources[j]) })
	fmt.Fprintf(&b, "\nResources (%d total, most expensive first):\n", len(resources))
	for i, res := range resources {
		if i == maxResources {
			fmt.Fprintf(&b, "... %d more not shown\n", len(resources)-maxResources)
			break
		}
		costText := res.CostDisplay
		if costText == "" {
			costText = "unpriced"
		}
		fmt.Fprintf(&b, "- %s %s %q (id %s) region=%s status=%s", res.Provider, res.Type, res.Name, res.ID, res.Region, res.Status)
		if res.Spec != "" {
			fmt.Fprintf(&b, " spec=%s", res.Spec)
		}
		fmt.Fprintf(&b, " cost=%s\n", costText)
	}
	if len(r.Recommendations) > 0 {
		b.WriteString("\nCloudRipper recommendations (not applied):\n")
		for _, rec := range r.Recommendations {
			fmt.Fprintf(&b, "- %s %s %q: %s, saves %s", rec.Action, rec.Type, rec.Name, rec.Reason, rec.SavingsDisplay)
			if rec.SuggestedSpec != "" {
				fmt.Fprintf(&b, " (suggested %s)", rec.SuggestedSpec)
			}
			b.WriteString("\n")
		}
		if r.PotentialSavingsDisplay != "" {
			fmt.Fprintf(&b, "Potential savings: %s\n", r.PotentialSavingsDisplay)
		}
	}
	for _, w := range r.Warnings {
		fmt.Fprintf(&b, "Warning: %s\n", w)
	}
	if len(samples) > 0 {
		b.WriteString("\nCost-watch samples (oldest first):\n")
		start := 0
		if len(samples) > 12 {
			start = len(samples) - 12
		}
		for _, s := range samples[start:] {
			fmt.Fprintf(&b, "- %s total=%s", s.At.UTC().Format("2006-01-02 15:04"), appruntime.FormatMoney(s.Total))
			keys := make([]string, 0, len(s.ByProvider))
			for k := range s.ByProvider {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Fprintf(&b, " %s=%s", k, appruntime.FormatMoney(s.ByProvider[k]))
			}
			b.WriteString("\n")
		}
		if len(samples) >= 2 {
			alerts := watch.Detect(samples[:len(samples)-1], samples[len(samples)-1], watch.NormalizeThreshold(threshold))
			if len(alerts) == 0 {
				b.WriteString("Spike check: no spike above the threshold in the latest sample.\n")
			}
			for _, a := range alerts {
				fmt.Fprintf(&b, "Spike: %s\n", a.Message)
			}
		}
	}
	return b.String()
}

func cost(r appruntime.ResourceView) float64 {
	if r.CostUSD == nil {
		return -1
	}
	return *r.CostUSD
}

// SystemPrompt tells the local model what it is and how to answer.
func SystemPrompt(task Task) string {
	base := `You are Nous ("` + Slogan + `"), the offline assistant inside CloudRipper, a local cloud inventory and cost tool. You run on the user's own computer through Ollama; nothing leaves the machine.
Use only the report below. Name the specific resources and dollar amounts. If the report does not contain the answer, say so. Keep it short: a few sentences or a short list. Never tell the user to run destructive commands; CloudRipper's own "optimize --apply" asks for confirmation.`
	switch task {
	case TaskSavings:
		return base + "\nTask: suggest the biggest savings first, with the estimated monthly amount and why. Mention idle, stopped, oversized, or unattached resources."
	case TaskSpikes:
		return base + "\nTask: explain the cost spikes in the cost-watch samples: which provider moved, by how much, and which resources in the report are the likely cause. If there are no samples, explain how to collect them with `cloudripper watch --state <file>`."
	}
	return base + "\nTask: answer the user's question about their inventory and costs."
}

// Question is the user turn for a task.
func Question(task Task, question string) string {
	q := strings.TrimSpace(question)
	switch task {
	case TaskSavings:
		if q == "" {
			return "Where can I save the most money?"
		}
	case TaskSpikes:
		if q == "" {
			return "Explain my cost spikes."
		}
	}
	return q
}

// Request is one Nous call.
type Request struct {
	Settings  Settings
	Endpoint  string
	Model     string // overrides Settings.Model when set
	Task      Task
	Question  string
	Report    appruntime.Report
	Samples   []watch.Snapshot
	Threshold float64
}

// Answer is what Nous said and which local model said it.
type Answer struct {
	Text  string `json:"text"`
	Model string `json:"model"`
	Task  Task   `json:"task"`
	Local bool   `json:"local"`
}

// Ask runs a task on local Ollama. It fails with ErrDisabled when Nous is
// off, before any data is read or sent anywhere.
func Ask(ctx context.Context, req Request) (Answer, error) {
	if !req.Settings.Enabled {
		return Answer{}, ErrDisabled
	}
	q := Question(req.Task, req.Question)
	if q == "" {
		return Answer{}, fmt.Errorf("ask Nous a question")
	}
	endpoint := req.Endpoint
	if endpoint == "" {
		endpoint = Endpoint()
	}
	client, err := NewClient(endpoint)
	if err != nil {
		return Answer{}, err
	}
	models, err := client.Models(ctx)
	if err != nil {
		return Answer{}, err
	}
	preferred := req.Model
	if preferred == "" {
		preferred = req.Settings.Model
	}
	model := PickModel(models, preferred)
	if model == "" {
		return Answer{}, ErrNoModel
	}
	messages := []Message{
		{Role: "system", Content: SystemPrompt(req.Task) + "\n\n" + BuildContext(req.Report, req.Samples, req.Threshold)},
		{Role: "user", Content: q},
	}
	text, err := client.Chat(ctx, model, messages)
	if err != nil {
		return Answer{}, err
	}
	return Answer{Text: text, Model: model, Task: req.Task, Local: true}, nil
}

// LoadSamples reads a cost-watch state file (cloudripper watch --state).
func LoadSamples(path string) ([]watch.Snapshot, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var st watch.State
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("read watch state %s: %w", path, err)
	}
	return st.Samples, nil
}
