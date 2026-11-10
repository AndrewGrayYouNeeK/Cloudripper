package output

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestParseAndCSV(t *testing.T) {
	format, err := Parse("JSON")
	if err != nil || format != FormatJSON {
		t.Fatalf("format %s err %v", format, err)
	}
	if _, err := Parse("yaml"); err == nil {
		t.Fatal("expected unknown format error")
	}
	var buf bytes.Buffer
	if err := WriteCSV(&buf, []string{"name"}, [][]string{{"=1+1"}, {"ok"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "'=1+1") {
		t.Fatalf("csv = %s", buf.String())
	}
	buf.Reset()
	if err := WriteJSON(&buf, map[string]string{"ok": "yes"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"ok": "yes"`) {
		t.Fatalf("json = %s", buf.String())
	}
	htmlFormat, err := Parse("HTML")
	if err != nil || htmlFormat != FormatHTML {
		t.Fatalf("html format %s err %v", htmlFormat, err)
	}
}

func TestColorTablesStayOutOfPlainOutput(t *testing.T) {
	headers := []string{"USD", "NOTE"}
	rows := [][]string{{"n/a", "unavailable"}, {"1.00", "ok"}}
	var plain bytes.Buffer
	if err := WriteTable(&plain, headers, rows); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plain.String(), "\033") {
		t.Fatalf("plain table has color: %q", plain.String())
	}
	var colored bytes.Buffer
	if err := WriteColorTable(&colored, headers, rows); err != nil {
		t.Fatal(err)
	}
	got := colored.String()
	if !strings.Contains(got, "\033[1;36m") || !strings.Contains(got, "\033[2mn/a\033[0m") || !strings.Contains(got, "\033[2munavailable\033[0m") {
		t.Fatalf("colored table = %q", got)
	}
	if strings.Contains(got, "\033[2m1.00") {
		t.Fatalf("colored a normal amount: %q", got)
	}

	var buf bytes.Buffer
	if ColorEnabled(&buf, false) {
		t.Fatal("a buffer is not a terminal")
	}
	if ColorEnabled(os.Stdout, true) {
		t.Fatal("--no-color must win")
	}
	t.Setenv("NO_COLOR", "1")
	if ColorEnabled(os.Stdout, false) {
		t.Fatal("NO_COLOR must disable color")
	}
}

func TestWriteHTMLEscapesAndStaysOffline(t *testing.T) {
	var buf bytes.Buffer
	err := WriteHTML(&buf, HTMLReport{
		Title:  `Costs <script>`,
		Banner: `FICTIONAL "sample"`,
		Notes:  []string{"Actual spend: unavailable"},
		Sections: []HTMLSection{{
			Heading: "Rows",
			Headers: []string{"NAME", "USD"},
			Rows:    [][]string{{`<script>alert(1)</script>`, "n/a"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	for _, needle := range []string{"<script", "<img", "<link", "http://", "https://", "src="} {
		if strings.Contains(got, needle) {
			t.Fatalf("html contains %q:\n%s", needle, got)
		}
	}
	for _, needle := range []string{"&lt;script&gt;", "unavailable", "n/a", "FICTIONAL"} {
		if !strings.Contains(got, needle) {
			t.Fatalf("html missing %q:\n%s", needle, got)
		}
	}
}
