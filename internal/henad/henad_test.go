package henad

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	appruntime "github.com/AndrewGrayYouNeeK/Cloudripper/internal/runtime"
	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/watch"
)

func TestPhraseMatches(t *testing.T) {
	for _, ok := range []string{"I understand", "i understand", "  I UNDERSTAND \n", "I Understand"} {
		if !PhraseMatches(ok) {
			t.Errorf("PhraseMatches(%q) = false", ok)
		}
	}
	for _, bad := range []string{"", "y", "yes", "I understand.", "I  understand", "understand", "I understood"} {
		if PhraseMatches(bad) {
			t.Errorf("PhraseMatches(%q) = true", bad)
		}
	}
}

func TestEnableNeedsPhraseAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "henad.json")
	s, err := Load(path)
	if err != nil || s.Enabled {
		t.Fatalf("default should be off, got %+v %v", s, err)
	}
	if _, err := Enable(path, "yes", "", time.Now()); !errors.Is(err, ErrNotConfirmed) {
		t.Fatalf("Enable(yes) err = %v, want ErrNotConfirmed", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a refused enable must not write the file")
	}
	now := time.Date(2026, 11, 26, 21, 0, 0, 0, time.UTC)
	if _, err := Enable(path, " i understand ", "llama3.2", now); err != nil {
		t.Fatal(err)
	}
	s, err = Load(path)
	if err != nil || !s.Enabled || s.Model != "llama3.2" || s.EnabledAt != "2026-11-26T21:00:00Z" {
		t.Fatalf("after enable: %+v %v", s, err)
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("settings mode = %v, want 0600", info.Mode().Perm())
		}
	}
	if err := Disable(path); err != nil {
		t.Fatal(err)
	}
	s, _ = Load(path)
	if s.Enabled || s.Model != "llama3.2" {
		t.Fatalf("after disable: %+v", s)
	}
}

func TestWarningSaysWhatAndWhere(t *testing.T) {
	text := strings.Join(WarningLines(), "\n")
	for _, want := range []string{"turning on Henad", "Ollama", "localhost", "What it reads", "Nothing is sent off this machine", "turn it off"} {
		if !strings.Contains(text, want) {
			t.Errorf("warning is missing %q", want)
		}
	}
}

func TestValidateEndpointLoopbackOnly(t *testing.T) {
	for _, ok := range []string{"http://localhost:11434", "http://127.0.0.1:11434", "http://[::1]:11434", "http://LOCALHOST:1"} {
		if err := ValidateEndpoint(ok); err != nil {
			t.Errorf("%s rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"https://api.openai.com", "http://192.168.1.5:11434", "http://localhost.evil.com", "ftp://127.0.0.1", "", "127.0.0.1:11434", "http://10.0.0.1"} {
		if err := ValidateEndpoint(bad); !errors.Is(err, ErrNotLocal) {
			t.Errorf("%q accepted", bad)
		}
	}
	if _, err := NewClient("https://example.com"); !errors.Is(err, ErrNotLocal) {
		t.Fatalf("NewClient accepted a remote host")
	}
}

func TestPickModel(t *testing.T) {
	models := []string{"nomic-embed-text:latest", "llama3.2:latest", "qwen2.5:7b"}
	if got := PickModel(models, "qwen2.5:7b"); got != "qwen2.5:7b" {
		t.Fatalf("got %q", got)
	}
	if got := PickModel(models, "llama3.2"); got != "llama3.2:latest" {
		t.Fatalf("got %q", got)
	}
	if got := PickModel(models, "missing"); got != "llama3.2:latest" {
		t.Fatalf("got %q", got)
	}
	if got := PickModel([]string{"nomic-embed-text"}, ""); got != "" {
		t.Fatalf("embedding model picked: %q", got)
	}
}

// fakeOllama records chat requests and answers like Ollama.
func fakeOllama(t *testing.T, models []string, seen *[]map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			list := []map[string]string{}
			for _, m := range models {
				list = append(list, map[string]string{"name": m})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"models": list})
		case "/api/chat":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			*seen = append(*seen, body)
			_ = json.NewEncoder(w).Encode(map[string]any{"message": map[string]string{"role": "assistant", "content": "Stop the idle VM to save $70/month."}, "done": true})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func demoReport(t *testing.T) appruntime.Report {
	t.Helper()
	r, err := appruntime.LoadDemoReport()
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestAskRefusesWhenOff(t *testing.T) {
	var seen []map[string]any
	srv := fakeOllama(t, []string{"llama3.2:latest"}, &seen)
	_, err := Ask(context.Background(), Request{Settings: Settings{}, Endpoint: srv.URL, Task: TaskSavings, Report: demoReport(t)})
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("err = %v, want ErrDisabled", err)
	}
	if len(seen) != 0 {
		t.Fatalf("Ollama was called while Henad is off")
	}
}

func TestAskSavingsUsesLocalModelAndReport(t *testing.T) {
	var seen []map[string]any
	srv := fakeOllama(t, []string{"nomic-embed-text", "llama3.2:latest"}, &seen)
	report := demoReport(t)
	ans, err := Ask(context.Background(), Request{Settings: Settings{Enabled: true}, Endpoint: srv.URL, Task: TaskSavings, Report: report})
	if err != nil {
		t.Fatal(err)
	}
	if ans.Model != "llama3.2:latest" || !ans.Local || !strings.Contains(ans.Text, "$70") {
		t.Fatalf("answer = %+v", ans)
	}
	if len(seen) != 1 || seen[0]["model"] != "llama3.2:latest" || seen[0]["stream"] != false {
		t.Fatalf("chat request = %+v", seen)
	}
	msgs := seen[0]["messages"].([]any)
	system := msgs[0].(map[string]any)["content"].(string)
	if !strings.Contains(system, "fictional demo sample") || !strings.Contains(system, report.Resources[0].Name) {
		t.Fatalf("system prompt lacks the report:\n%s", system)
	}
	if user := msgs[1].(map[string]any)["content"].(string); user != "Where can I save the most money?" {
		t.Fatalf("user turn = %q", user)
	}
}

func TestAskOllamaDownAndNoModel(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	_, err := Ask(context.Background(), Request{Settings: Settings{Enabled: true}, Endpoint: url, Task: TaskAsk, Question: "hi"})
	if !errors.Is(err, ErrOllamaDown) {
		t.Fatalf("err = %v, want ErrOllamaDown", err)
	}
	var seen []map[string]any
	empty := fakeOllama(t, nil, &seen)
	_, err = Ask(context.Background(), Request{Settings: Settings{Enabled: true}, Endpoint: empty.URL, Task: TaskAsk, Question: "hi"})
	if !errors.Is(err, ErrNoModel) {
		t.Fatalf("err = %v, want ErrNoModel", err)
	}
}

func TestClientDoesNotFollowRedirects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://example.com/steal", http.StatusFound)
	}))
	defer srv.Close()
	c, err := NewClient(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Models(context.Background()); err == nil {
		t.Fatal("redirect was followed")
	}
}

func TestBuildContextWithSpikes(t *testing.T) {
	base := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	samples := []watch.Snapshot{
		{At: base, Total: 100, ByProvider: map[string]float64{"aws": 80, "gcp": 20}},
		{At: base.Add(time.Hour), Total: 180, ByProvider: map[string]float64{"aws": 160, "gcp": 20}},
	}
	text := BuildContext(demoReport(t), samples, 25)
	for _, want := range []string{"Cost-watch samples", "aws=", "Spike:", "Resources ("} {
		if !strings.Contains(text, want) {
			t.Errorf("context missing %q:\n%s", want, text)
		}
	}
	path := filepath.Join(t.TempDir(), "watch.json")
	data, _ := json.Marshal(watch.State{Samples: samples})
	_ = os.WriteFile(path, data, 0o600)
	got, err := LoadSamples(path)
	if err != nil || len(got) != 2 {
		t.Fatalf("LoadSamples = %v, %v", got, err)
	}
}

func TestParseTask(t *testing.T) {
	for in, want := range map[string]Task{"": TaskAsk, "ask": TaskAsk, "Savings": TaskSavings, "spikes": TaskSpikes} {
		got, err := ParseTask(in)
		if err != nil || got != want {
			t.Errorf("ParseTask(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := ParseTask("apply"); err == nil {
		t.Error("ParseTask(apply) accepted")
	}
}

func TestStatusAndEnableTyped(t *testing.T) {
	var seen []map[string]any
	srv := fakeOllama(t, []string{"llama3.2:latest"}, &seen)
	t.Setenv(EnvEndpoint, srv.URL)
	path := filepath.Join(t.TempDir(), "henad.json")
	st := CurrentStatus(context.Background(), path)
	if st.Enabled || st.OllamaRunning || st.Phrase != ConfirmPhrase || len(st.Warning) == 0 {
		t.Fatalf("off status = %+v", st)
	}
	if _, err := EnableTyped(context.Background(), path, "I understan", ""); !errors.Is(err, ErrNotConfirmed) {
		t.Fatalf("partial phrase err = %v", err)
	}
	st, err := EnableTyped(context.Background(), path, "I understand", "")
	if err != nil || !st.Enabled || !st.OllamaRunning || st.Model != "llama3.2:latest" {
		t.Fatalf("on status = %+v %v", st, err)
	}
}

func TestSettingsPathKeepsPreRenameFile(t *testing.T) {
	dir := t.TempDir()
	if got := settingsPathIn(dir); got != filepath.Join(dir, "henad.json") {
		t.Fatalf("fresh install: got %s", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "nous.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := settingsPathIn(dir); got != filepath.Join(dir, "nous.json") {
		t.Fatalf("pre-rename file: got %s", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "henad.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := settingsPathIn(dir); got != filepath.Join(dir, "henad.json") {
		t.Fatalf("both files: got %s", got)
	}
}

func TestEndpointReadsPreRenameVariable(t *testing.T) {
	t.Setenv("CLOUDRIPPER_HENAD_OLLAMA", "")
	t.Setenv("CLOUDRIPPER_NOUS_OLLAMA", "http://127.0.0.1:9999")
	if got := Endpoint(); got != "http://127.0.0.1:9999" {
		t.Fatalf("got %s", got)
	}
}
