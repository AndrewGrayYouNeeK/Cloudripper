package bills

import (
	"strings"
	"testing"
	"time"
)

func TestMatchFixtureEmails(t *testing.T) {
	when := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	fixtures := []struct {
		from    string
		subject string
		body    string
		id      string
		account string
		amount  float64
	}{
		{"no-reply@amazonaws.com", "Amazon Web Services Invoice", "Account ID: 1234-5678-9012\nAmount due: $42.50", "aws", "1234-5678-9012", 42.50},
		{"billing@cloud.google.com", "Google Cloud billing statement", "Project: my-project-1\nTotal: $10.00", "gcp", "my-project-1", 10},
		{"billing@azure.com", "Microsoft Azure invoice", "Subscription ID: 11111111-2222-3333-4444-555555555555\nAmount due: $8.25", "azure", "11111111-2222-3333-4444-555555555555", 8.25},
		{"billing@digitalocean.com", "DigitalOcean invoice", "Amount due: $15.00", "digitalocean", "", 15},
		{"billing@linode.com", "Linode invoice", "Total: $5.00", "linode", "", 5},
		{"billing@akamai.com", "Akamai invoice", "Total: $6.00", "linode", "", 6},
		{"invoice@vercel.com", "Vercel receipt", "Total: $20.00", "vercel", "", 20},
		{"billing@netlify.com", "Netlify invoice", "Total: $9.00", "netlify", "", 9},
		{"billing@heroku.com", "Heroku invoice", "Total: $7.00", "heroku", "", 7},
		{"billing@cloudflare.com", "Cloudflare invoice", "Total: $11.00", "cloudflare", "", 11},
		{"billing@supabase.com", "Supabase invoice", "Total: $25.00", "supabase", "", 25},
		{"noreply@firebase.google.com", "Firebase invoice", "Total: $3.50", "firebase", "", 3.50},
		{"billing@mongodb.com", "MongoDB Atlas invoice", "Total: $57.00", "mongodb", "", 57},
		{"billing@openai.com", "OpenAI receipt", "Amount due: $30.00", "openai", "", 30},
	}
	for _, fixture := range fixtures {
		hit, ok := MatchEmail(Message{
			From: fixture.from, Subject: fixture.subject, Body: fixture.body, Date: when, Source: "imap",
		})
		if !ok || hit.Provider.ID != fixture.id || hit.Account != fixture.account || hit.Amount != fixture.amount {
			t.Fatalf("%s: got %+v ok=%v", fixture.id, hit, ok)
		}
	}
}

func TestShippingMailIsIgnored(t *testing.T) {
	_, ok := MatchEmail(Message{From: "auto-confirm@amazon.com", Subject: "Your package was delivered", Body: "Order 123"})
	if ok {
		t.Fatal("retail mail matched a cloud provider")
	}
}

func TestLatestBillWins(t *testing.T) {
	messages := []Message{
		{From: "a@amazonaws.com", Subject: "invoice", Body: "Amount due: $10.00\nAccount ID: 1234-5678-9012", Date: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), Source: "imap"},
		{From: "a@amazonaws.com", Subject: "invoice", Body: "Amount due: $42.50\nAccount ID: 1234-5678-9012", Date: time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC), Source: "imap"},
	}
	findings := Report(messages, nil, Accounts{AWS: true}, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if len(findings) != 1 || findings[0].Amount != 42.50 || findings[0].Status != StatusConfigured {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestCSVMerchants(t *testing.T) {
	parsed, err := ParseCSV(strings.NewReader(strings.Join([]string{
		"Date,Description,Amount",
		"2026-09-02,DIGITALOCEAN.COM,-15.00",
		"2026-09-03,AMAZON WEB SERVICES,-42.50",
		"2026-08-01,STARBUCKS,-4.50",
		"2026-09-04,LINODE,-5.00",
	}, "\n")))
	if err != nil {
		t.Fatal(err)
	}
	findings := Report(nil, parsed, Accounts{AWS: true}, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	byID := map[string]Finding{}
	for _, finding := range findings {
		byID[finding.ProviderID] = finding
	}
	if _, ok := byID["starbucks"]; ok {
		t.Fatal("starbucks should not match")
	}
	if byID["digitalocean"].Status != StatusForgotten || byID["digitalocean"].Amount != 15 {
		t.Fatalf("digitalocean = %+v", byID["digitalocean"])
	}
	if byID["aws"].Status != StatusConfigured || byID["aws"].Amount != 42.50 || byID["aws"].Source != "csv" {
		t.Fatalf("aws = %+v", byID["aws"])
	}
	if byID["linode"].Status != StatusForgotten {
		t.Fatalf("linode = %+v", byID["linode"])
	}
}

func TestOldMailIsDropped(t *testing.T) {
	msg := Message{
		From: "billing@vercel.com", Subject: "Vercel invoice", Body: "Total: $20.00",
		Date: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC), Source: "gmail",
	}
	findings := Report([]Message{msg}, nil, Accounts{}, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}
