package output

import (
	"bytes"
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
