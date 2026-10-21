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
}
