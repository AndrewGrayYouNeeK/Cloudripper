package demo

import (
	"os"
	"testing"

	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/cloud"
)

func TestLoadIsFictionalAndPriced(t *testing.T) {
	ds, res, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !ds.Fictional || ds.Disclaimer == "" {
		t.Fatalf("dataset not labeled fictional: %+v", ds.Disclaimer)
	}
	if res.UnpricedCount != 5 {
		t.Fatalf("unpriced = %d", res.UnpricedCount)
	}
	if !res.ActualAvailable {
		t.Fatal("expected fictional snapshots to mark actuals available")
	}
	var priced float64
	providers := map[string]bool{}
	for _, r := range res.Resources {
		providers[r.Provider] = true
		if r.CostSource == cloud.CostSourceUnpriced {
			if r.CostPriced() {
				t.Fatalf("%s should be unpriced", r.ID)
			}
			continue
		}
		priced += r.CostUSD
	}
	for _, name := range []string{"aws", "gcp", "azure", "cloudflare"} {
		if !providers[name] {
			t.Fatalf("missing provider %s", name)
		}
	}
	if res.EstimatedTotal != priced {
		t.Fatalf("estimated %v priced sum %v", res.EstimatedTotal, priced)
	}
	if res.EstimatedTotal != 617 {
		t.Fatalf("estimated total = %v", res.EstimatedTotal)
	}
	if res.ActualTotal != 337.55 {
		t.Fatalf("actual total = %v", res.ActualTotal)
	}
}

func TestCardExportMatchesDocs(t *testing.T) {
	got, err := CardExport()
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("../../docs/examples/sample-card-export.csv")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("embedded card export differs from docs/examples/sample-card-export.csv")
	}
}

func TestParseDatasetRejectsUnlabeled(t *testing.T) {
	_, err := parseDataset([]byte(`{"fictional":false,"disclaimer":"FICTIONAL","resources":[{"id":"x","provider":"aws","type":"ec2"}]}`))
	if err == nil {
		t.Fatal("expected fictional flag rejection")
	}
	_, err = parseDataset([]byte(`{"fictional":true,"disclaimer":"just a sample","resources":[{"id":"x","provider":"aws","type":"ec2","cost_usd":1,"cost_source":"estimate"}]}`))
	if err == nil {
		t.Fatal("expected disclaimer rejection")
	}
}
