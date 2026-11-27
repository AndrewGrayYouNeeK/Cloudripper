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

	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/nous"
	appruntime "github.com/AndrewGrayYouNeeK/Cloudripper/internal/runtime"
)

// useNous points the settings file at a temp dir and fakes the terminal check.
func useNous(t *testing.T, terminal bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "nous.json")
	t.Setenv(nous.EnvConfig, path)
	prev := nousStdinIsTerminal
	nousStdinIsTerminal = func(io.Reader) bool { return terminal }
	t.Cleanup(func() { nousStdinIsTerminal = prev })
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

func nousEnabled(t *testing.T, path string) bool {
	t.Helper()
	s, err := nous.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return s.Enabled
}

func TestNousIsOffByDefault(t *testing.T) {
	useNous(t, true)
	out, err := runCLIWithInput(t, "", "nous", "status")
	if err != nil || !strings.Contains(out, "OFF (default)") {
		t.Fatalf("status: %q %v", out, err)
	}
	_, err = runCLIWithInput(t, "", "nous", "savings", "--demo")
	if !errors.Is(err, nous.ErrDisabled) {
		t.Fatalf("savings while off: err = %v", err)
	}
}

func TestNousEnableRequiresTypedPhrase(t *testing.T) {
	path := useNous(t, true)
	for _, reply := range []string{"y\n", "yes\n", "\n", "I understand.\n", ""} {
		out, err := runCLIWithInput(t, reply, "nous", "enable")
		if err != nil {
			t.Fatalf("reply %q: %v", reply, err)
		}
		if !strings.Contains(out, "Turn on Nous?") || !strings.Contains(out, "Nothing is sent off this machine") {
			t.Fatalf("warning not shown:\n%s", out)
		}
		if !strings.Contains(out, "Cancelled. Nous stays off.") || nousEnabled(t, path) {
			t.Fatalf("reply %q turned Nous on:\n%s", reply, out)
		}
	}
	out, err := runCLIWithInput(t, "  i UNDERSTAND \n", "nous", "enable")
	if err != nil || !strings.Contains(out, "Nous is on") || !nousEnabled(t, path) {
		t.Fatalf("typed phrase did not enable: %q %v", out, err)
	}
	out, err = runCLIWithInput(t, "", "nous", "disable")
	if err != nil || !strings.Contains(out, "Nous is off") || nousEnabled(t, path) {
		t.Fatalf("disable: %q %v", out, err)
	}
}

func TestNousEnableNonInteractive(t *testing.T) {
	path := useNous(t, false)
	_, err := runCLIWithInput(t, "I understand\n", "nous", "enable")
	if err == nil || !strings.Contains(err.Error(), "--i-understand") || nousEnabled(t, path) {
		t.Fatalf("non-terminal enable without flag: err = %v", err)
	}
	// The global --yes is not consent for Nous.
	_, err = runCLIWithInput(t, "", "--yes", "nous", "enable")
	if err == nil || nousEnabled(t, path) {
		t.Fatalf("--yes turned Nous on")
	}
	out, err := runCLIWithInput(t, "", "nous", "enable", "--i-understand", "--model", "llama3.2")
	if err != nil || !nousEnabled(t, path) || !strings.Contains(out, "Turn on Nous?") {
		t.Fatalf("--i-understand: %q %v", out, err)
	}
}

func TestNousAskAnswersFromLocalOllama(t *testing.T) {
	path := useNous(t, false)
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			_, _ = w.Write([]byte(`{"models":[{"name":"llama3.2:latest"}]}`))
		case "/api/chat":
			var body struct {
				Messages []nous.Message `json:"messages"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			asked = append(asked, body.Messages[len(body.Messages)-1].Content)
			_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"Your biggest cost is the GPU VM."}}`))
		}
	}))
	defer srv.Close()
	t.Setenv(nous.EnvEndpoint, srv.URL)
	if _, err := nous.Enable(path, nous.ConfirmPhrase, "", nousNow()); err != nil {
		t.Fatal(err)
	}
	prev := nousReport
	nousReport = func(context.Context, bool) (appruntime.Report, error) { return appruntime.LoadDemoReport() }
	t.Cleanup(func() { nousReport = prev })

	out, err := runCLIWithInput(t, "", "nous", "ask", "What", "costs", "most?", "--demo")
	if err != nil || !strings.Contains(out, "Nous (offline · llama3.2:latest)") || !strings.Contains(out, "GPU VM") {
		t.Fatalf("ask: %q %v", out, err)
	}
	if len(asked) != 1 || asked[0] != "What costs most?" {
		t.Fatalf("question sent = %v", asked)
	}
	t.Setenv(nous.EnvEndpoint, "https://api.example.com")
	if _, err := runCLIWithInput(t, "", "nous", "ask", "hi", "--demo"); !errors.Is(err, nous.ErrNotLocal) {
		t.Fatalf("remote endpoint err = %v", err)
	}
}
