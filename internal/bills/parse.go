package bills

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Message is one email. Body is kept in memory for parsing and is not logged.
type Message struct {
	From    string
	Subject string
	Date    time.Time
	Body    string
	Source  string
}

// Hit is a provider match before amounts from the same account are collapsed.
type Hit struct {
	Provider Provider
	Account  string
	Amount   float64
	When     time.Time
	Source   string
}

var amountPattern = regexp.MustCompile(`(?i)(?:usd|us\$|\$)\s*([0-9]{1,3}(?:,[0-9]{3})*(?:\.[0-9]{2})|[0-9]+(?:\.[0-9]{2})?)`)

var billWords = []string{"invoice", "billing", "receipt", "payment", "statement", "amount due", "charged"}

// MatchEmail reports the provider for a billing message.
func MatchEmail(msg Message) (Hit, bool) {
	from := strings.ToLower(msg.From)
	blob := strings.ToLower(msg.Subject + "\n" + msg.Body)
	var found Provider
	ok := false
	for _, provider := range Catalog() {
		if domainHit(from, provider.Domains) {
			found = provider
			ok = true
			break
		}
	}
	if !ok && looksLikeBill(blob) {
		for _, provider := range Catalog() {
			if keywordHit(blob, provider.Keywords) {
				found = provider
				ok = true
				break
			}
		}
	}
	if !ok {
		return Hit{}, false
	}
	amount, _ := extractAmount(msg.Subject + "\n" + msg.Body)
	return Hit{
		Provider: found,
		Account:  extractAccount(found, msg.Subject+"\n"+msg.Body),
		Amount:   amount,
		When:     msg.Date,
		Source:   msg.Source,
	}, true
}

func domainHit(from string, domains []string) bool {
	for _, domain := range domains {
		if domain != "" && strings.Contains(from, strings.ToLower(domain)) {
			return true
		}
	}
	return false
}

func keywordHit(text string, keywords []string) bool {
	for _, keyword := range keywords {
		if containsKeyword(text, keyword) {
			return true
		}
	}
	return false
}

func containsKeyword(text, keyword string) bool {
	text = normalizeMerchant(text)
	keyword = normalizeMerchant(keyword)
	if keyword == "" || !strings.Contains(text, keyword) {
		return false
	}
	if len(keyword) <= 4 {
		return hasToken(text, keyword)
	}
	return true
}

func hasToken(text, token string) bool {
	for _, field := range strings.Fields(text) {
		if field == token {
			return true
		}
	}
	return false
}

func looksLikeBill(text string) bool {
	for _, word := range billWords {
		if strings.Contains(text, word) {
			return true
		}
	}
	return false
}

func normalizeMerchant(value string) string {
	value = strings.ToLower(value)
	value = strings.NewReplacer("*", " ", "_", " ", "-", " ").Replace(value)
	return strings.Join(strings.Fields(value), " ")
}

func extractAccount(provider Provider, text string) string {
	if provider.Account == nil {
		return ""
	}
	match := provider.Account.FindStringSubmatch(text)
	if len(match) < 2 {
		return ""
	}
	return match[1]
}

func extractAmount(text string) (float64, bool) {
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "total") || strings.Contains(lower, "amount due") || strings.Contains(lower, "charged") {
			if amount, ok := firstAmount(line); ok {
				return amount, true
			}
		}
	}
	return firstAmount(text)
}

func firstAmount(text string) (float64, bool) {
	match := amountPattern.FindStringSubmatch(text)
	if len(match) < 2 {
		return 0, false
	}
	raw := strings.ReplaceAll(match[1], ",", "")
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, false
	}
	if value < 0 {
		value = -value
	}
	return value, true
}
