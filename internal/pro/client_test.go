package pro

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewClientValidation(t *testing.T) {
	cases := []struct {
		url, token, want string
	}{
		{"", "crp_x", EnvURL},
		{"http://pro.example.com", "crp_x", "https"},
		{"https://pro.example.com", "", EnvToken},
		{"https://pro.example.com", "ghp_x", "does not look like"},
	}
	for _, tc := range cases {
		if _, err := NewClient(tc.url, tc.token); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("NewClient(%q, token) error = %v, want %q", tc.url, err, tc.want)
		}
	}
	c, err := NewClient("http://localhost:8787/", "crp_abc")
	if err != nil || c.BaseURL != "http://localhost:8787" {
		t.Fatalf("localhost http should be allowed: %v %+v", err, c)
	}
}

func TestUploadSendsTokenAndEnvelope(t *testing.T) {
	var got Upload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/reports" || r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer crp_test" {
			t.Errorf("missing bearer token")
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("bad body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true,"report":{"id":"r1","kind":"rip","account":{"id":"a1","label":"prod"},"resource_count":2,"estimated_total":12.5}}`))
	}))
	defer srv.Close()
	c, err := NewClient(strings.Replace(srv.URL, "127.0.0.1", "localhost", 1), "crp_test")
	if err != nil {
		t.Fatal(err)
	}
	stored, err := c.Upload(context.Background(), Upload{Account: "prod", Kind: "rip", Report: json.RawMessage(`{"resources":[]}`)})
	if err != nil {
		t.Fatal(err)
	}
	if stored.ID != "r1" || stored.Account.Label != "prod" || *stored.ResourceCount != 2 || *stored.EstimatedTotal != 12.5 {
		t.Fatalf("unexpected stored: %+v", stored)
	}
	if got.Account != "prod" || got.Kind != "rip" || string(got.Report) != `{"resources":[]}` {
		t.Fatalf("unexpected envelope: %+v", got)
	}
}

func TestUploadErrors(t *testing.T) {
	status := http.StatusPaymentRequired
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"ok":false,"error":"CloudRipper Pro subscription is not active."}`))
	}))
	defer srv.Close()
	c, _ := NewClient(srv.URL, "crp_test")
	_, err := c.Upload(context.Background(), Upload{Account: "prod", Report: json.RawMessage(`{}`)})
	if err == nil || !strings.Contains(err.Error(), "not active") || !IsPermanent(err) {
		t.Fatalf("want permanent 402 error, got %v", err)
	}
	status = http.StatusBadGateway
	_, err = c.Upload(context.Background(), Upload{Account: "prod", Report: json.RawMessage(`{}`)})
	if err == nil || IsPermanent(err) {
		t.Fatalf("5xx should be retryable, got %v", err)
	}
}

func TestUploadRejectsOversizedReport(t *testing.T) {
	c, _ := NewClient("https://pro.example.com", "crp_test")
	big := `{"resources":[],"pad":"` + strings.Repeat("x", MaxReportBytes) + `"}`
	if _, err := c.Upload(context.Background(), Upload{Account: "prod", Report: json.RawMessage(big)}); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("want size error, got %v", err)
	}
}

func TestInferKind(t *testing.T) {
	for in, want := range map[string]string{
		`{"resources":[]}`:       "rip",
		`{"Recommendations":[]}`: "optimize",
		`{"recommendations":[]}`: "optimize",
	} {
		if got, err := InferKind([]byte(in)); err != nil || got != want {
			t.Fatalf("InferKind(%s) = %q, %v", in, got, err)
		}
	}
	if _, err := InferKind([]byte(`{"x":1}`)); err == nil {
		t.Fatal("want error for unknown shape")
	}
	if _, err := InferKind([]byte(`[1]`)); err == nil {
		t.Fatal("want error for non-object")
	}
}
