package output

import (
	"io"
	"os"
)

// ColorEnabled reports whether table output should use ANSI color.
// Color is off when noColor is set, when NO_COLOR is a non-empty environment
// variable, or when w is not a terminal. Callers must not use this for
// json, csv, or html.
func ColorEnabled(w io.Writer, noColor bool) bool {
	if noColor || os.Getenv("NO_COLOR") != "" {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
