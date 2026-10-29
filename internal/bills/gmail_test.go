package bills

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestGmailGetOnly(t *testing.T) {
	body := base64.RawURLEncoding.EncodeToString([]byte("Amount due: $20.00\n"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("authorization missing or wrong")
		}
		switch {
		case strings.Contains(r.URL.Path, "/messages/") && !strings.HasSuffix(r.URL.Path, "/messages"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "m1",
				"payload": map[string]any{
					"mimeType": "text/plain",
					"headers": []map[string]string{
						{"name": "From", "value": "invoice@vercel.com"},
						{"name": "Subject", "value": "Vercel invoice"},
						{"name": "Date", "value": "Tue, 01 Sep 2026 12:00:00 +0000"},
					},
					"body": map[string]string{"data": body},
				},
			})
		default:
			if r.URL.Query().Get("q") == "" {
				t.Error("missing query")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"messages": []map[string]string{{"id": "m1"}},
			})
		}
	}))
	defer server.Close()

	path := t.TempDir() + "/token.json"
	if err := os.WriteFile(path, []byte(`{"access_token":"test-token"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	messages, err := (Gmail{
		TokenPath: path,
		BaseURL:   server.URL,
		Client:    server.Client(),
		Since:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}).Messages(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Source != "gmail" || !strings.Contains(messages[0].From, "vercel.com") {
		t.Fatalf("messages = %+v", messages)
	}
	hit, ok := MatchEmail(messages[0])
	if !ok || hit.Provider.ID != "vercel" || hit.Amount != 20 {
		t.Fatalf("hit = %+v ok=%v", hit, ok)
	}
}
