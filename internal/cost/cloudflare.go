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
// Required token permission: Billing Read
// (https://developers.cloudflare.com/api/resources/billing/subresources/history/methods/list/)
//
// Caveats (as of the Cloudflare API docs used for this integration):
//   - Rows are billing-history / invoice items, not resource-level usage meters.
//   - Workers Analytics, GraphQL usage, or dashboard graphs are NOT treated as spend.
//   - Only USD amounts inside the trailing Window() are summed.
//   - Non-USD currencies are skipped and reported as warnings.
//   - Missing Billing Read yields an unavailable warning, never a fabricated $0 actual.
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
		return Snapshot{}, fmt.Errorf("cloudflare billing history unavailable (needs Account Billing Read): %w", err)
	}
	nowFn := time.Now
	if c.Now != nil {
		nowFn = c.Now
	}
	start, end := Window(nowFn())
	return ParseCloudflareBillingHistory(items, start, end), nil
}

// ParseCloudflareBillingHistory sums USD charge amounts inside [start, end).
func ParseCloudflareBillingHistory(items []cloud.CloudflareBillingItem, start, end time.Time) Snapshot {
	snap := Snapshot{
		Source: cloud.CostSourceCloudflareBilling,
		Warnings: []string{
			"cloudflare billing-history is invoice/charge rows from GET /accounts/{account_id}/billing/history, not per-resource usage; resource rows keep estimate ($0) unless a future export matches them",
		},
	}
	serviceTotals := map[string]float64{}
	for _, item := range items {
		occurred, err := cloud.ParseCloudflareTime(item.OccurredAt)
		if err != nil {
			continue
		}
		if occurred.Before(start) || !occurred.Before(end) {
			continue
		}
		currency := strings.TrimSpace(item.Currency)
		if currency != "" && !strings.EqualFold(currency, "USD") {
			snap.Warnings = appendWarning(snap.Warnings, fmt.Sprintf("skipped non-USD cloudflare billing item %s (%s)", item.ID, currency))
			continue
		}
		amount := item.Amount
		// Prefer explicit charge-like rows; still accept amount when type is empty
		// because the API examples use type=charge with amount set.
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
