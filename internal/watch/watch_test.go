package watch

import (
	"context"
	"testing"
	"time"
)

type seqCollector struct {
	samples []Snapshot
	i       int
}

func (s *seqCollector) Collect(context.Context) (Snapshot, error) {
	snap := s.samples[s.i]
	s.i++
	return snap, nil
}

func TestSpikeAlertsOnceAndPersists(t *testing.T) {
	path := t.TempDir() + "/watch.json"
	collector := &seqCollector{samples: []Snapshot{
		{Total: 100, ByProvider: map[string]float64{"aws": 100}},
		{Total: 150, ByProvider: map[string]float64{"aws": 150}},
		{Total: 155, ByProvider: map[string]float64{"aws": 155}},
	}}
	watcher, err := New(Options{
		Interval:  time.Minute,
		Threshold: 25,
		StatePath: path,
		Collector: collector,
	})
	if err != nil {
		t.Fatal(err)
	}
	alerts, err := watcher.Tick(context.Background())
	if err != nil || len(alerts) != 0 {
		t.Fatalf("first alerts %v err %v", alerts, err)
	}
	alerts, err = watcher.Tick(context.Background())
	if err != nil || len(alerts) != 2 {
		t.Fatalf("second alerts %v err %v", alerts, err)
	}
	alerts, err = watcher.Tick(context.Background())
	if err != nil || len(alerts) != 0 {
		t.Fatalf("third alerts %v err %v", alerts, err)
	}

	reloaded, err := New(Options{Interval: time.Minute, Threshold: 0.25, StatePath: path, Collector: collector})
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.state.Samples) != 3 {
		t.Fatalf("samples = %d", len(reloaded.state.Samples))
	}
}

func TestZeroBaselineDoesNotAlert(t *testing.T) {
	alerts := Detect([]Snapshot{{Total: 0}}, Snapshot{Total: 40}, 0.25)
	if len(alerts) != 0 {
		t.Fatalf("alerts = %+v", alerts)
	}
}
