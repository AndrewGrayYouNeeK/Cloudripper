package audit

import (
	"testing"
	"time"
)

func TestWriteAndRead(t *testing.T) {
	path := t.TempDir() + "/audit.log"
	logger := &Logger{Path: path, Now: func() time.Time { return time.Unix(10, 0).UTC() }}
	if err := logger.Write(Event{Command: "optimize", Action: "terminate", Outcome: "applied", ResourceID: "i-1"}); err != nil {
		t.Fatal(err)
	}
	if err := logger.Write(Event{Command: "optimize", Outcome: "dry-run", DryRun: true}); err != nil {
		t.Fatal(err)
	}
	events, err := ReadAll(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].ResourceID != "i-1" || !events[1].DryRun {
		t.Fatalf("events = %+v", events)
	}
	if err := (*Logger)(nil).Write(Event{}); err != nil {
		t.Fatal(err)
	}
}
