package bills

import (
	"encoding/csv"
	"fmt"
	"io"
	"strings"
	"time"
)

// Transaction is one row from a bank or card export.
type Transaction struct {
	Date        time.Time
	Description string
	Amount      float64
}

// ParseCSV reads a headered export. It understands Date, Description/Merchant,
// and Amount, or separate Debit and Credit columns. Charges are returned as
// positive amounts.
func ParseCSV(r io.Reader) ([]Transaction, error) {
	reader := csv.NewReader(r)
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = -1
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("csv: %w", err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("csv: empty file")
	}
	cols := mapColumns(rows[0])
	if cols.date < 0 || cols.desc < 0 || (cols.amount < 0 && cols.debit < 0 && cols.credit < 0) {
		return nil, fmt.Errorf("csv: need date, description, and amount or debit/credit columns")
	}
	var txns []Transaction
	for _, row := range rows[1:] {
		if len(strings.TrimSpace(strings.Join(row, ""))) == 0 {
			continue
		}
		when, err := parseDate(field(row, cols.date))
		if err != nil {
			return nil, fmt.Errorf("csv date %q: %w", field(row, cols.date), err)
		}
		amount, err := rowAmount(row, cols)
		if err != nil {
			return nil, fmt.Errorf("csv amount: %w", err)
		}
		if amount == 0 {
			continue
		}
		if amount < 0 {
			amount = -amount
		}
		txns = append(txns, Transaction{
			Date:        when,
			Description: field(row, cols.desc),
			Amount:      amount,
		})
	}
	return txns, nil
}

type columns struct {
	date   int
	desc   int
	amount int
	debit  int
	credit int
}

func mapColumns(header []string) columns {
	cols := columns{date: -1, desc: -1, amount: -1, debit: -1, credit: -1}
	for i, name := range header {
		switch normalizeMerchant(name) {
		case "date", "transaction date", "posted date", "posting date", "trans date":
			cols.date = i
		case "description", "merchant", "name", "memo", "narrative", "details":
			cols.desc = i
		case "amount", "amt":
			cols.amount = i
		case "debit", "withdrawal", "charge":
			cols.debit = i
		case "credit", "deposit":
			cols.credit = i
		}
	}
	return cols
}

func field(row []string, i int) string {
	if i < 0 || i >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[i])
}

func rowAmount(row []string, cols columns) (float64, error) {
	if cols.debit >= 0 || cols.credit >= 0 {
		debit, err := parseMoney(field(row, cols.debit))
		if err != nil {
			return 0, err
		}
		credit, err := parseMoney(field(row, cols.credit))
		if err != nil {
			return 0, err
		}
		if debit != 0 {
			return abs(debit), nil
		}
		return abs(credit), nil
	}
	return parseMoney(field(row, cols.amount))
}

func parseMoney(value string) (float64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	value = strings.TrimPrefix(value, "$")
	value = strings.ReplaceAll(value, ",", "")
	value = strings.TrimSpace(value)
	negative := false
	if strings.HasPrefix(value, "(") && strings.HasSuffix(value, ")") {
		negative = true
		value = strings.Trim(value, "()")
	}
	var parsed float64
	if _, err := fmt.Sscan(value, &parsed); err != nil {
		return 0, fmt.Errorf("%q", value)
	}
	if negative {
		parsed = -parsed
	}
	return parsed, nil
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func parseDate(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	layouts := []string{
		"2006-01-02",
		"2006/01/02",
		"01/02/2006",
		"1/2/2006",
		"02-Jan-2006",
		"Jan 2, 2006",
		"January 2, 2006",
		time.RFC3339,
	}
	for _, layout := range layouts {
		if when, err := time.Parse(layout, value); err == nil {
			return when.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized date")
}

// MatchTransaction maps a card description onto the catalog.
func MatchTransaction(txn Transaction) (Hit, bool) {
	text := normalizeMerchant(txn.Description)
	if !looksLikeMerchant(text) {
		return Hit{}, false
	}
	for _, provider := range Catalog() {
		if keywordHit(text, provider.Keywords) {
			return Hit{
				Provider: provider,
				Amount:   abs(txn.Amount),
				When:     txn.Date,
				Source:   "csv",
			}, true
		}
	}
	return Hit{}, false
}

func looksLikeMerchant(text string) bool {
	return strings.TrimSpace(text) != ""
}
