package schedule

import (
	"testing"
	"time"
)

func TestInOffWindow(t *testing.T) {
	night := time.Date(2026, 1, 2, 19, 0, 0, 0, time.UTC)
	morning := time.Date(2026, 1, 2, 7, 0, 0, 0, time.UTC)
	off, err := InOffWindow(DefaultOffWindow, night)
	if err != nil || !off {
		t.Fatalf("19:00 off=%v err=%v", off, err)
	}
	off, err = InOffWindow(DefaultOffWindow, morning)
	if err != nil || off {
		t.Fatalf("07:00 should be outside the window, off=%v err=%v", off, err)
	}
	if _, err := InOffWindow("always", night); err == nil {
		t.Fatal("expected unsupported schedule error")
	}
}
