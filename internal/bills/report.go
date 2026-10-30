package bills

import (
	"sort"
	"time"
)

const (
	// StatusConfigured means CloudRipper already scans this provider.
	StatusConfigured = "configured"
	// StatusForgotten means a bill was found and CloudRipper has no account for it.
	StatusForgotten = "possibly forgotten"
)

// Finding is the latest bill for one provider account.
type Finding struct {
	Provider   string    `json:"provider"`
	ProviderID string    `json:"provider_id"`
	Account    string    `json:"account,omitempty"`
	Amount     float64   `json:"amount"`
	Currency   string    `json:"currency,omitempty"`
	Date       time.Time `json:"date,omitempty"`
	Configured bool      `json:"configured"`
	Status     string    `json:"status"`
	Source     string    `json:"source"`
}

// Report keeps the latest bill per provider and account.
// Messages and transactions dated before since are ignored when the date is set.
func Report(messages []Message, txns []Transaction, accounts Accounts, since time.Time) []Finding {
	var hits []Hit
	for _, msg := range messages {
		if !since.IsZero() && !msg.Date.IsZero() && msg.Date.Before(since) {
			continue
		}
		hit, ok := MatchEmail(msg)
		if !ok {
			continue
		}
		hits = append(hits, hit)
	}
	for _, txn := range txns {
		if !since.IsZero() && !txn.Date.IsZero() && txn.Date.Before(since) {
			continue
		}
		hit, ok := MatchTransaction(txn)
		if !ok {
			continue
		}
		hits = append(hits, hit)
	}
	latest := map[string]Finding{}
	for _, hit := range hits {
		key := hit.Provider.ID + "\x00" + hit.Account
		next := Finding{
			Provider:   hit.Provider.Name,
			ProviderID: hit.Provider.ID,
			Account:    hit.Account,
			Amount:     hit.Amount,
			Currency:   "USD",
			Date:       hit.When,
			Configured: accounts.Has(hit.Provider.ID),
			Source:     hit.Source,
		}
		if next.Configured {
			next.Status = StatusConfigured
		} else {
			next.Status = StatusForgotten
		}
		prev, ok := latest[key]
		if !ok || newer(next, prev) {
			latest[key] = next
		}
	}
	out := make([]Finding, 0, len(latest))
	for _, finding := range latest {
		out = append(out, finding)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Provider == out[j].Provider {
			return out[i].Account < out[j].Account
		}
		return out[i].Provider < out[j].Provider
	})
	return out
}

func newer(next, prev Finding) bool {
	if next.Date.After(prev.Date) {
		return true
	}
	if next.Date.Before(prev.Date) {
		return false
	}
	return prev.Amount == 0 && next.Amount > 0
}
