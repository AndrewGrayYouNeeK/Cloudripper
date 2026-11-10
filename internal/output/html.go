package output

import (
	"fmt"
	"html"
	"io"
	"strings"
)

// HTMLReport is a single self-contained HTML document.
// Callers put display text in the fields; WriteHTML escapes every value.
type HTMLReport struct {
	Title    string
	Banner   string
	Notes    []string
	Sections []HTMLSection
}

// HTMLSection is one heading plus an optional table.
type HTMLSection struct {
	Heading string
	Notes   []string
	Headers []string
	Rows    [][]string
}

// WriteHTML writes one HTML document with inline CSS and no external
// assets, scripts, or network requests. Every title, note, header, and cell
// is escaped.
func WriteHTML(w io.Writer, report HTMLReport) error {
	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n<meta charset=\"utf-8\">\n")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	b.WriteString("<meta http-equiv=\"Content-Security-Policy\" content=\"default-src 'none'; style-src 'unsafe-inline'; img-src 'none'; script-src 'none'; connect-src 'none'\">\n")
	b.WriteString("<title>")
	b.WriteString(html.EscapeString(report.Title))
	b.WriteString("</title>\n<style>\n")
	b.WriteString(htmlCSS)
	b.WriteString("</style>\n</head>\n<body>\n")
	b.WriteString("<main>\n<h1>")
	b.WriteString(html.EscapeString(report.Title))
	b.WriteString("</h1>\n")
	if strings.TrimSpace(report.Banner) != "" {
		b.WriteString("<p class=\"banner\" role=\"note\">")
		b.WriteString(html.EscapeString(report.Banner))
		b.WriteString("</p>\n")
	}
	writeNotes(&b, report.Notes)
	for _, section := range report.Sections {
		if strings.TrimSpace(section.Heading) != "" {
			b.WriteString("<h2>")
			b.WriteString(html.EscapeString(section.Heading))
			b.WriteString("</h2>\n")
		}
		writeNotes(&b, section.Notes)
		if len(section.Headers) == 0 && len(section.Rows) == 0 {
			continue
		}
		b.WriteString("<div class=\"table-wrap\"><table>\n<thead><tr>")
		for _, header := range section.Headers {
			b.WriteString("<th>")
			b.WriteString(html.EscapeString(header))
			b.WriteString("</th>")
		}
		b.WriteString("</tr></thead>\n<tbody>\n")
		if len(section.Rows) == 0 {
			b.WriteString("<tr><td colspan=\"")
			b.WriteString(fmt.Sprintf("%d", len(section.Headers)))
			b.WriteString("\">No rows.</td></tr>\n")
		}
		for _, row := range section.Rows {
			b.WriteString("<tr>")
			for _, cell := range row {
				b.WriteString("<td>")
				b.WriteString(html.EscapeString(cell))
				b.WriteString("</td>")
			}
			b.WriteString("</tr>\n")
		}
		b.WriteString("</tbody></table></div>\n")
	}
	b.WriteString("</main>\n</body>\n</html>\n")
	_, err := io.WriteString(w, b.String())
	return err
}

func writeNotes(b *strings.Builder, notes []string) {
	for _, note := range notes {
		if strings.TrimSpace(note) == "" {
			continue
		}
		b.WriteString("<p>")
		b.WriteString(html.EscapeString(note))
		b.WriteString("</p>\n")
	}
}

const htmlCSS = `body{margin:0;background:#0b1020;color:#e6edf7;font:16px/1.5 system-ui,-apple-system,"Segoe UI",Roboto,Helvetica,Arial,sans-serif}
main{max-width:1100px;margin:0 auto;padding:24px}
h1{font-size:1.6rem;margin:0 0 12px}
h2{font-size:1.15rem;margin:28px 0 8px}
p{margin:8px 0;color:#c7d7f5}
.banner{background:#3b2a0a;color:#fde68a;border:1px solid #a16207;border-radius:10px;padding:12px 14px;font-weight:650}
.table-wrap{overflow-x:auto}
table{border-collapse:collapse;width:100%;background:#131c38}
th,td{border:1px solid #25315a;padding:6px 8px;text-align:left;vertical-align:top;font:13px/1.4 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace}
th{background:#1a2650;color:#e6edf7}
`
