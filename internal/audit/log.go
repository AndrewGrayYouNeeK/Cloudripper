package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Event is one JSON line in the audit log.
type Event struct {
	Time         time.Time `json:"time"`
	Command      string    `json:"command"`
	Action       string    `json:"action,omitempty"`
	Provider     string    `json:"provider,omitempty"`
	ResourceID   string    `json:"resource_id,omitempty"`
	ResourceName string    `json:"resource_name,omitempty"`
	Outcome      string    `json:"outcome"`
	Detail       string    `json:"detail,omitempty"`
	DryRun       bool      `json:"dry_run"`
}

// Logger appends audit events as JSON lines.
type Logger struct {
	Path string
	Now  func() time.Time

	mu sync.Mutex
}

// Write appends one event. A zero time is filled from Now or time.Now.
func (l *Logger) Write(e Event) error {
	if l == nil || l.Path == "" {
		return nil
	}
	if e.Time.IsZero() {
		if l.Now != nil {
			e.Time = l.Now()
		} else {
			e.Time = time.Now().UTC()
		}
	}
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(l.Path), 0o755); err != nil && filepath.Dir(l.Path) != "." {
		return err
	}
	f, err := os.OpenFile(l.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("audit log: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	return nil
}

// ReadAll parses every JSON line in the log.
func ReadAll(path string) ([]Event, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var events []Event
	start := 0
	for i, b := range data {
		if b != '\n' {
			continue
		}
		line := data[start:i]
		start = i + 1
		if len(line) == 0 {
			continue
		}
		var e Event
		if err := json.Unmarshal(line, &e); err != nil {
			return events, fmt.Errorf("audit line: %w", err)
		}
		events = append(events, e)
	}
	return events, nil
}
