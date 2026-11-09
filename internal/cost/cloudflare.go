package cost

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/cloud"
)

// CloudflareBillingAPI fetches account billing history.
type CloudflareBillingAPI interface {
	ListBillingHistory(ctx context.Context) ([]cloud.CloudflareBillingItem, error)
}

// CloudflareBilling reads invoice/billing-history charges for one account.
//
// Source: GET /accounts/{account_id}/billing/history
// Auth: API token Bearer (preferred) or legacy email+key per Cloudflare API docs
// Required token permission: Billing Read (Account > Billing > Read)
// Docs:
//   - https://developers.cloudflare.com/api/resources/billing/subresources/history/methods/list/
//   - https://developers.cloudflare.com/billing/understand/billing-permissions/
//
// Semantics / limitations:
//   - Rows are billing-history / invoice items, not resource-level usage meters.
//   - The list endpoint has no date-range query; CloudRipper pages results and
//     keeps items whose occurred_at falls in Window() [start, end).
//   - Only items with currency exactly USD are summed; missing/other currencies
//     are skipped with warnings (never assumed USD).
//   - Workers Analytics / GraphQL usage are never treated as invoiced spend.
//   - Auth/permission failures return an error so Resolve keeps ActualAvailable
//     false for this source (never a fabricated $0 actual).
//   - Per-resource Cloudflare rows stay CostSourceUnpriced; this snapshot is
//     account-level only.
type CloudflareBilling struct {
	API CloudflareBillingAPI
	Now func() time.Time
}

// NewCloudflareBilling builds a billing source from account credentials.
func NewCloudflareBilling(accountID, token string) *CloudflareBilling {
	return &CloudflareBilling{
		API: &cloud.CloudflareAPI{
			AccountID: strings.TrimSpace(accountID),
			Token:     strings.TrimSpace(token),
		},
	}
}

// Name implements Source.
func (c *CloudflareBilling) Name() string { return cloud.CostSourceCloudflareBilling }

// Fetch implements Source.
func (c *CloudflareBilling) Fetch(ctx context.Context) (Snapshot, error) {
	if c == nil || c.API == nil {
		return Snapshot{}, fmt.Errorf("cloudflare billing client not configured")
	}
	items, err := c.API.ListBillingHistory(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("cloudflare billing history unavailable (needs Account Billing Read on the API token): %w", err)
	}
	nowFn := time.Now
	if c.Now != nil {
		nowFn = c.Now
	}
	start, end := Window(nowFn())
	return ParseCloudflareBillingHistory(items, start, end), nil
}

// ParseCloudflareBillingHistory sums USD invoice/charge amounts with
// occurred_at in [start, end).
func ParseCloudflareBillingHistory(items []cloud.CloudflareBillingItem, start, end time.Time) Snapshot {
	snap := Snapshot{
		Source: cloud.CostSourceCloudflareBilling,
		Warnings: []string{
			"cloudflare billing-history is account invoice/charge rows from GET /accounts/{account_id}/billing/history filtered by occurred_at; not per-resource usage. Cloudflare inventory rows stay unpriced",
		},
	}
	serviceTotals := map[string]float64{}
	for _, item := range items {
		occurred, err := cloud.ParseCloudflareTime(item.OccurredAt)
		if err != nil {
			snap.Warnings = appendWarning(snap.Warnings, fmt.Sprintf("skipped cloudflare billing item %s: invalid occurred_at", item.ID))
			continue
		}
		if occurred.Before(start) || !occurred.Before(end) {
			continue
		}
		currency := strings.TrimSpace(item.Currency)
		if !strings.EqualFold(currency, "USD") {
			label := currency
			if label == "" {
				label = "missing"
			}
			snap.Warnings = appendWarning(snap.Warnings, fmt.Sprintf("skipped non-USD cloudflare billing item %s (%s)", item.ID, label))
			continue
		}
		amount := item.Amount
		if strings.EqualFold(item.Type, "credit") || strings.EqualFold(item.Type, "refund") {
			amount = -absFloat(amount)
		}
		label := firstNonEmpty(item.Description, item.Action, item.Type, "billing-item")
		serviceTotals[label] += amount
		snap.TotalUSD += amount
	}
	for service, usd := range serviceTotals {
		snap.Services = append(snap.Services, ServiceCost{Service: service, USD: usd})
	}
	return snap
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
