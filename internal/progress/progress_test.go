package progress

import (
	"sync"
	"testing"
	"time"
)

func TestReporterThrottlesAndForces(t *testing.T) {
	now := time.Unix(0, 0)
	var mu sync.Mutex
	var events []Event
	reporter := NewReporterWithInterval(func(event Event) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
	}, time.Second)
	reporter.now = func() time.Time { return now }
	reporter.Start()

	reporter.Report(Event{Phase: PhaseScanning})
	reporter.Report(Event{Phase: PhaseScanning})
	if len(events) != 1 {
		t.Fatalf("throttled reports = %d, want 1", len(events))
	}

	now = now.Add(2 * time.Second)
	reporter.Report(Event{Phase: PhaseTransferring})
	if len(events) != 2 {
		t.Fatalf("reports after interval = %d, want 2", len(events))
	}
	if events[1].ElapsedMS != 2000 {
		t.Fatalf("elapsed_ms = %d, want 2000", events[1].ElapsedMS)
	}
	if events[0].Version != SchemaVersion || events[1].Version != SchemaVersion {
		t.Fatalf("events missing schema version: %#v", events)
	}

	reporter.ReportNow(Event{Phase: PhaseDone})
	if len(events) != 3 {
		t.Fatalf("forced report = %d, want 3", len(events))
	}
}

func TestReporterNilSinkIsNoop(t *testing.T) {
	reporter := NewReporter(nil)
	reporter.Start()
	reporter.Report(Event{Phase: PhaseScanning})
	reporter.ReportNow(Event{Phase: PhaseDone})
	if reporter.Enabled() {
		t.Fatal("reporter with nil sink reported enabled")
	}
}
