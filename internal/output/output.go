package output

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// Format selects how command results are rendered.
type Format string

const (
	FormatTable Format = "table"
	FormatJSON  Format = "json"
	FormatCSV   Format = "csv"
	FormatHTML  Format = "html"
)

// Parse resolves a format name.
func Parse(value string) (Format, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "table", "text":
		return FormatTable, nil
	case "json":
		return FormatJSON, nil
	case "csv":
		return FormatCSV, nil
	case "html":
		return FormatHTML, nil
	default:
		return "", fmt.Errorf("unknown output format %q (want table, json, csv, or html)", value)
	}
}

// WriteJSON writes indented JSON.
func WriteJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// WriteCSV writes a header row and records. Cells that start with =, +, -, or @
// are prefixed so spreadsheet apps do not treat them as formulas.
func WriteCSV(w io.Writer, headers []string, rows [][]string) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(headers); err != nil {
		return err
	}
	for _, row := range rows {
		safe := make([]string, len(row))
		for i, cell := range row {
			safe[i] = neutralize(cell)
		}
		if err := cw.Write(safe); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// WriteTable writes an aligned text table with no ANSI color.
func WriteTable(w io.Writer, headers []string, rows [][]string) error {
	return writeTable(w, headers, rows, false)
}

// WriteColorTable writes an aligned text table and then colors the header
// and the tokens n/a and unavailable. Alignment is computed before the
// escape codes are added. JSON, CSV, and HTML must not call this.
func WriteColorTable(w io.Writer, headers []string, rows [][]string) error {
	return writeTable(w, headers, rows, true)
}

func writeTable(w io.Writer, headers []string, rows [][]string, color bool) error {
	var buf bytes.Buffer
	tw := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, strings.Join(headers, "\t")); err != nil {
		return err
	}
	for _, row := range rows {
		if _, err := fmt.Fprintln(tw, strings.Join(row, "\t")); err != nil {
			return err
		}
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	text := buf.String()
	if color {
		text = colorizeTable(text)
	}
	_, err := io.WriteString(w, text)
	return err
}

func colorizeTable(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if line == "" {
			continue
		}
		if i == 0 {
			lines[i] = "\033[1;36m" + line + "\033[0m"
			continue
		}
		lines[i] = colorizeToken(colorizeToken(line, "unavailable"), "n/a")
	}
	return strings.Join(lines, "\n")
}

func colorizeToken(line, token string) string {
	if !strings.Contains(line, token) {
		return line
	}
	return strings.ReplaceAll(line, token, "\033[2m"+token+"\033[0m")
}

func neutralize(cell string) string {
	if cell == "" {
		return cell
	}
	switch cell[0] {
	case '=', '+', '-', '@':
		return "'" + cell
	default:
		return cell
	}
}
