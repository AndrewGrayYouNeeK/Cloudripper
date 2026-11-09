package cost

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/cloud"
)

type fakeCFBilling struct {
	items []cloud.CloudflareBillingItem
	err   error
}

func (f fakeCFBilling) ListBillingHistory(context.Context) ([]cloud.CloudflareBillingItem, error) {
	return f.items, f.err
}

func TestCloudflareBillingHistoryWindowAndCurrency(t *testing.T) {
	now := time.Date(2026, 11, 9, 15, 0, 0, 0, time.UTC)
	src := &CloudflareBilling{
		API: fakeCFBilling{items: []cloud.CloudflareBillingItem{
			{ID: "1", Amount: 12.5, Currency: "USD", Description: "Workers Paid", Type: "charge", OccurredAt: "2026-11-01T12:00:00Z"},
			{ID: "2", Amount: 99, Currency: "EUR", Description: "Euro plan", Type: "charge", OccurredAt: "2026-11-02T12:00:00Z"},
			{ID: "3", Amount: 5, Currency: "USD", Description: "Old invoice", Type: "charge", OccurredAt: "2026-09-01T12:00:00Z"},
			{ID: "4", Amount: 2, Currency: "USD", Description: "Credit", Type: "credit", OccurredAt: "2026-11-03T12:00:00Z"},
		}},
		Now: func() time.Time { return now },
	}
	snap, err := src.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snap.Source != cloud.CostSourceCloudflareBilling {
		t.Fatalf("source = %s", snap.Source)
	}
	if snap.TotalUSD != 10.5 { // 12.5 - 2
		t.Fatalf("total = %v", snap.TotalUSD)
	}
	joined := strings.Join(snap.Warnings, "\n")
	if !strings.Contains(joined, "invoice/charge") || !strings.Contains(joined, "non-USD") {
		t.Fatalf("warnings = %v", snap.Warnings)
	}
	if len(snap.Resources) != 0 {
		t.Fatalf("must not invent resource-level spend: %+v", snap.Resources)
	}
}

func TestCloudflareBillingUnavailable(t *testing.T) {
	src := &CloudflareBilling{API: fakeCFBilling{err: errors.New("permission denied")}}
	_, err := src.Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Billing Read") {
		t.Fatalf("err = %v", err)
	}
	res := Resolve(context.Background(), []cloud.Resource{{
		ID: "w", Provider: "cloudflare", Type: cloud.ResourceCFWorker, CostUSD: 0, CostSource: cloud.CostSourceEstimate,
	}}, []Source{src})
	if res.ActualAvailable {
		t.Fatal("missing billing must not report actual available")
	}
	if res.ActualTotal != 0 {
		t.Fatalf("actual total = %v", res.ActualTotal)
	}
	if res.Resources[0].CostSource != cloud.CostSourceEstimate {
		t.Fatalf("resource source = %s", res.Resources[0].CostSource)
	}
	if len(res.Warnings) == 0 {
		t.Fatal("expected warning")
	}
}

func TestCloudflareBillingLabeledSeparately(t *testing.T) {
	src := &CloudflareBilling{
		API: fakeCFBilling{items: []cloud.CloudflareBillingItem{
			{ID: "1", Amount: 3, Currency: "USD", Description: "R2", Type: "charge", OccurredAt: time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339)},
		}},
	}
	res := Resolve(context.Background(), []cloud.Resource{{
		ID: "bucket", Provider: "cloudflare", Type: cloud.ResourceCFR2, CostUSD: 0, CostSource: cloud.CostSourceEstimate,
	}}, []Source{src})
	if !res.ActualAvailable || res.ActualTotal != 3 {
		t.Fatalf("res = %+v", res)
	}
	if res.Resources[0].CostUSD != 0 || res.Resources[0].CostSource != cloud.CostSourceEstimate {
		t.Fatalf("resource must keep estimate without resource-level match: %+v", res.Resources[0])
	}
	summary := Summary(res)
	if !strings.Contains(summary, cloud.CostSourceCloudflareBilling) {
		t.Fatalf("summary = %s", summary)
	}
}
