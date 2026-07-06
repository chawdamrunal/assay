package api

import (
	"sync"

	"github.com/chawdamrunal/assay/internal/scanner"
)

// ScanRunner manages in-flight scans and broadcasts progress events to any
// number of subscribers. The v0.5.x design used a single shared channel
// per scan — that broke when both the LiveScanPage SSE and the global
// background-scan tracker tried to consume the same scan, because each
// event would go to whichever consumer happened to be ready first.
//
// Concurrency model:
//
//  1. Emit fans out the event to every subscriber's channel under the
//     scan's mutex. Each subscriber channel is buffered + non-blocking send
//     so a slow consumer drops events instead of stalling the producer.
//  2. Complete closes every subscriber channel and marks the scan closed
//     so future Subscribe calls return nil.
//  3. Unsubscribe removes a single channel from the list — used by the SSE
//     handler when the HTTP client disconnects.
type ScanRunner struct {
	mu     sync.Mutex
	active map[string]*scanState
}

type scanState struct {
	mu     sync.Mutex
	subs   []chan scanner.Event
	closed bool
}

// NewScanRunner returns an empty ScanRunner.
func NewScanRunner() *ScanRunner {
	return &ScanRunner{active: map[string]*scanState{}}
}

// Register starts tracking a scan. The returned channel is the producer's
// initial subscriber slot — kept for backward compatibility with callers
// that expected Register to immediately yield a channel they could read.
// Most production code paths use Subscribe instead.
func (s *ScanRunner) Register(scanID string) <-chan scanner.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.active[scanID]
	if !ok {
		st = &scanState{}
		s.active[scanID] = st
	}
	ch := make(chan scanner.Event, 64)
	st.mu.Lock()
	st.subs = append(st.subs, ch)
	st.mu.Unlock()
	return ch
}

// Subscribe adds a new consumer to an active scan and returns its dedicated
// channel. Multiple subscribers each receive every event — events are
// broadcast, not load-balanced. Returns nil when the scan has completed
// (subscribers join "after the fact" should poll the on-disk audit.json
// instead).
func (s *ScanRunner) Subscribe(scanID string) <-chan scanner.Event {
	s.mu.Lock()
	st, ok := s.active[scanID]
	s.mu.Unlock()
	if !ok {
		return nil
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.closed {
		return nil
	}
	ch := make(chan scanner.Event, 64)
	st.subs = append(st.subs, ch)
	return ch
}

// Unsubscribe removes a previously-returned channel from the fan-out list.
// Safe to call on a channel that's already been removed (idempotent). The
// SSE handler calls this on client disconnect so a long-running scan
// doesn't accumulate stale subscriber channels for dropped clients.
func (s *ScanRunner) Unsubscribe(scanID string, ch <-chan scanner.Event) {
	s.mu.Lock()
	st, ok := s.active[scanID]
	s.mu.Unlock()
	if !ok {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	out := st.subs[:0]
	for _, c := range st.subs {
		if (<-chan scanner.Event)(c) == ch {
			close(c)
			continue
		}
		out = append(out, c)
	}
	st.subs = out
}

// Emit broadcasts an event to every subscriber. Non-blocking sends — slow
// consumers drop events instead of stalling the scanner.
func (s *ScanRunner) Emit(scanID string, e scanner.Event) {
	s.mu.Lock()
	st, ok := s.active[scanID]
	s.mu.Unlock()
	if !ok {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.closed {
		return
	}
	for _, ch := range st.subs {
		select {
		case ch <- e:
		default:
			// Slow consumer — drop event.
		}
	}
}

// Complete marks the scan finished, closes every subscriber channel, and
// removes the scan from the active map. The map delete prevents unbounded
// memory growth on a long-lived `assay serve`: without it, every scan (and
// every fleet member, every re-run) retained a scanState entry forever, and
// Subscribe/Emit/IsActive iterated that ever-growing map on each call.
func (s *ScanRunner) Complete(scanID string) {
	s.mu.Lock()
	st, ok := s.active[scanID]
	if ok {
		delete(s.active, scanID)
	}
	s.mu.Unlock()
	if !ok {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.closed {
		return
	}
	st.closed = true
	for _, ch := range st.subs {
		close(ch)
	}
	st.subs = nil
}

// IsActive reports whether the scan is currently in-flight.
func (s *ScanRunner) IsActive(scanID string) bool {
	s.mu.Lock()
	st, ok := s.active[scanID]
	s.mu.Unlock()
	if !ok {
		return false
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	return !st.closed
}
