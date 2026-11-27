package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/henad"
	appruntime "github.com/AndrewGrayYouNeeK/Cloudripper/internal/runtime"
)

// useHenad points the settings file at a temp dir and fakes the terminal check.
func useHenad(t *testing.T, terminal bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "henad.json")
	t.Setenv(henad.EnvConfig, path)
	prev := henadStdinIsTerminal
	henadStdinIsTerminal = func(io.Reader) bool { return terminal }
	t.Cleanup(func() { henadStdinIsTerminal = prev })
	return path
}

func runCLIWithInput(t *testing.T, input string, args ...string) (string, error) {
	t.Helper()
	resetCmdFlags(t)
	out := &bytes.Buffer{}
	rootCmd.SetOut(out)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetIn(strings.NewReader(input))
	rootCmd.SetArgs(args)
	err := rootCmd.Execute()
	return out.String(), err
}

func henadEnabled(t *testing.T, path string) bool {
	t.Helper()
	s, err := henad.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return s.Enabled
}

func TestHenadIsOffByDefault(t *testing.T) {
	useHenad(t, true)
	out, err := runCLIWithInput(t, "", "henad", "status")
	if err != nil || !strings.Contains(out, "OFF (default)") {
		t.Fatalf("status: %q %v", out, err)
	}
	_, err = runCLIWithInput(t, "", "henad", "savings", "--demo")
	if !errors.Is(err, henad.ErrDisabled) {
		t.Fatalf("savings while off: err = %v", err)
	}
}

func TestHenadEnableRequiresTypedPhrase(t *testing.T) {
	path := useHenad(t, true)
	for _, reply := range []string{"y\n", "yes\n", "\n", "I understand.\n", ""} {
		out, err := runCLIWithInput(t, reply, "henad", "enable")
		if err != nil {
			t.Fatalf("reply %q: %v", reply, err)
		}
		if !strings.Contains(out, "Turn on Henad?") || !strings.Contains(out, "Nothing is sent off this machine") {
			t.Fatalf("warning not shown:\n%s", out)
		}
		if !strings.Contains(out, "Cancelled. Henad stays off.") || henadEnabled(t, path) {
			t.Fatalf("reply %q turned Henad on:\n%s", reply, out)
		}
	}
	out, err := runCLIWithInput(t, "  i UNDERSTAND \n", "henad", "enable")
	if err != nil || !strings.Contains(out, "Henad is on") || !henadEnabled(t, path) {
		t.Fatalf("typed phrase did not enable: %q %v", out, err)
	}
	out, err = runCLIWithInput(t, "", "henad", "disable")
	if err != nil || !strings.Contains(out, "Henad is off") || henadEnabled(t, path) {
		t.Fatalf("disable: %q %v", out, err)
	}
}

func TestHenadEnableNonInteractive(t *testing.T) {
	path := useHenad(t, false)
	_, err := runCLIWithInput(t, "I understand\n", "henad", "enable")
	if err == nil || !strings.Contains(err.Error(), "--i-understand") || henadEnabled(t, path) {
		t.Fatalf("non-terminal enable without flag: err = %v", err)
	}
	// The global --yes is not consent for Henad.
	_, err = runCLIWithInput(t, "", "--yes", "henad", "enable")
	if err == nil || henadEnabled(t, path) {
		t.Fatalf("--yes turned Henad on")
	}
	out, err := runCLIWithInput(t, "", "henad", "enable", "--i-understand", "--model", "llama3.2")
	if err != nil || !henadEnabled(t, path) || !strings.Contains(out, "Turn on Henad?") {
		t.Fatalf("--i-understand: %q %v", out, err)
	}
}

func TestHenadAskAnswersFromLocalOllama(t *testing.T) {
	path := useHenad(t, false)
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			_, _ = w.Write([]byte(`{"models":[{"name":"llama3.2:latest"}]}`))
		case "/api/chat":
			var body struct {
				Messages []henad.Message `json:"messages"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			asked = append(asked, body.Messages[len(body.Messages)-1].Content)
			_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"Your biggest cost is the GPU VM."}}`))
		}
	}))
	defer srv.Close()
	t.Setenv(henad.EnvEndpoint, srv.URL)
	if _, err := henad.Enable(path, henad.ConfirmPhrase, "", henadNow()); err != nil {
		t.Fatal(err)
	}
	prev := henadReport
	henadReport = func(context.Context, bool) (appruntime.Report, error) { return appruntime.LoadDemoReport() }
	t.Cleanup(func() { henadReport = prev })

	out, err := runCLIWithInput(t, "", "henad", "ask", "What", "costs", "most?", "--demo")
	if err != nil || !strings.Contains(out, "Henad (offline · llama3.2:latest)") || !strings.Contains(out, "GPU VM") {
		t.Fatalf("ask: %q %v", out, err)
	}
	if len(asked) != 1 || asked[0] != "What costs most?" {
		t.Fatalf("question sent = %v", asked)
	}
	t.Setenv(henad.EnvEndpoint, "https://api.example.com")
	if _, err := runCLIWithInput(t, "", "henad", "ask", "hi", "--demo"); !errors.Is(err, henad.ErrNotLocal) {
		t.Fatalf("remote endpoint err = %v", err)
	}
}
