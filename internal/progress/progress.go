// Package progress defines the structured progress events emitted by winsuck
// transfers and a throttled reporter that forwards them to a caller-supplied
// sink (for example a JSON encoder on stderr).
package progress

import (
	"sync"
	"time"
)

type Phase string

// SchemaVersion identifies the progress event contract. It is bumped when a
// change would break existing consumers.
const SchemaVersion = 1

const (
	PhaseScanning     Phase = "scanning"
	PhaseTransferring Phase = "transferring"
	PhaseExtracting   Phase = "extracting"
	PhaseDone         Phase = "done"
	PhaseError        Phase = "error"
)

// Event is a single progress observation. Totals are zero until the sender has
// finished scanning the source tree, so consumers should treat zero totals as
// "not yet known" and fall back to an indeterminate status.
type Event struct {
	Version    int    `json:"v"`
	Phase      Phase  `json:"phase"`
	FilesDone  int64  `json:"files_done"`
	FilesTotal int64  `json:"files_total,omitempty"`
	BytesDone  int64  `json:"bytes_done"`
	BytesTotal int64  `json:"bytes_total,omitempty"`
	Skipped    int64  `json:"skipped,omitempty"`
	Current    string `json:"current,omitempty"`
	ElapsedMS  int64  `json:"elapsed_ms,omitempty"`
	Error      string `json:"error,omitempty"`
}

// Sink receives progress events. Implementations must be safe for concurrent
// use because transfers may report from multiple goroutines.
type Sink func(Event)

// Reporter throttles events before forwarding them to a Sink. Phase changes and
// terminal events should be reported with ReportNow so a status bar always sees
// them immediately. A nil Sink disables reporting.
type Reporter struct {
	mu       sync.Mutex
	sink     Sink
	interval time.Duration
	last     time.Time
	start    time.Time
	now      func() time.Time
}

const DefaultInterval = 100 * time.Millisecond

// NewReporter returns a Reporter that emits at most once per interval. A nil
// sink yields a no-op reporter.
func NewReporter(sink Sink) *Reporter {
	return NewReporterWithInterval(sink, DefaultInterval)
}

func NewReporterWithInterval(sink Sink, interval time.Duration) *Reporter {
	if interval <= 0 {
		interval = DefaultInterval
	}
	return &Reporter{sink: sink, interval: interval, now: time.Now}
}

// Enabled reports whether a sink is configured.
func (r *Reporter) Enabled() bool {
	return r != nil && r.sink != nil
}

// Start resets the elapsed clock.
func (r *Reporter) Start() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.start = r.now()
	r.last = time.Time{}
	r.mu.Unlock()
}

// Report forwards the event only if enough time has passed since the previous
// emission.
func (r *Reporter) Report(event Event) {
	r.emit(event, false)
}

// ReportNow forwards the event immediately, ignoring the throttle interval.
func (r *Reporter) ReportNow(event Event) {
	r.emit(event, true)
}

func (r *Reporter) emit(event Event, force bool) {
	if !r.Enabled() {
		return
	}
	r.mu.Lock()
	now := r.now()
	if r.start.IsZero() {
		r.start = now
	}
	if !force && now.Sub(r.last) < r.interval {
		r.mu.Unlock()
		return
	}
	r.last = now
	event.Version = SchemaVersion
	event.ElapsedMS = now.Sub(r.start).Milliseconds()
	sink := r.sink
	r.mu.Unlock()
	sink(event)
}
