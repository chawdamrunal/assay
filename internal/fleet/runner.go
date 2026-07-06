package fleet

import (
	"context"
	"path/filepath"
	"sync"
	"time"

	assaymcp "github.com/chawdamrunal/assay/internal/mcp"
)

// tailDrainGrace is how long tailers are allowed to keep draining a member's
// events.jsonl after all workers finish, before they are force-canceled. It
// lets healthy members flush their final events while guaranteeing a member
// that crashed without writing its terminal `done` event cannot wedge the
// fleet forever.
const tailDrainGrace = 2 * time.Second

// EventSink receives one ProgressEvent at a time. The fleet runner uses two
// of these: a disk-write sink that appends to events.jsonl (so SSE clients
// that connect mid-flight can replay history) and a broadcast sink that
// pushes to any live SSE subscribers.
type EventSink interface {
	OnFleetEvent(scanID string, ev assaymcp.ProgressEvent)
}

// Broadcaster fans an event out to multiple subscribers. Each subscriber
// gets its own buffered channel so a slow consumer cannot stall others.
//
// Used by the HTTP layer to multiplex one fleet's event stream to N
// concurrent SSE clients without making the runner know about HTTP.
type Broadcaster struct {
	mu          sync.Mutex
	subscribers []chan Event
	closed      bool
}

// Event is the wire shape pushed to subscribers and serialized into
// events.jsonl. ScanID identifies which member produced the event so the UI
// can route updates to the right per-plugin card.
type Event struct {
	ScanID  string `json:"scan_id"`
	Stage   string `json:"stage"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	At      string `json:"at,omitempty"`
}

// NewBroadcaster returns an empty broadcaster.
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{}
}

// Subscribe returns a new buffered channel that receives every Event
// published after subscription. Caller must drain or close-loop via the
// returned unsubscribe function to avoid leaks.
func (b *Broadcaster) Subscribe(buffer int) (<-chan Event, func()) {
	if buffer <= 0 {
		buffer = 64
	}
	ch := make(chan Event, buffer)
	b.mu.Lock()
	if b.closed {
		close(ch)
		b.mu.Unlock()
		return ch, func() {}
	}
	b.subscribers = append(b.subscribers, ch)
	b.mu.Unlock()
	unsub := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		for i, s := range b.subscribers {
			if s == ch {
				b.subscribers = append(b.subscribers[:i], b.subscribers[i+1:]...)
				close(ch)
				return
			}
		}
	}
	return ch, unsub
}

// Publish pushes one event to every active subscriber. Non-blocking — a
// subscriber whose buffer is full silently drops the event (matches the
// trade-off used by api.ScanRunner: progress is informational, not load-bearing).
func (b *Broadcaster) Publish(ev Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, s := range b.subscribers {
		select {
		case s <- ev:
		default:
		}
	}
}

// Close drains and closes all subscriber channels. After Close, Subscribe
// returns an already-closed channel.
func (b *Broadcaster) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for _, s := range b.subscribers {
		close(s)
	}
	b.subscribers = nil
}

// Runner drives parallel scans of fleet members. It is intentionally generic
// over how an individual scan runs — the StartScan func is injected so the
// same runner serves both legacy and MCP modes (and tests using FakeClient).
//
// Lifecycle:
//  1. NewRunner(store, scansDir, broadcaster)
//  2. Run(ctx, fleetID, members, parallel, startScan) — blocks until all
//     members terminate, returns aggregate Report.
//
// On every member-scan event (read from that scan's events.jsonl via
// TailEvents), the runner does two things: persists to <fleet_dir>/events.jsonl
// via the store, and publishes to the broadcaster for live SSE consumers.
type Runner struct {
	store       *Store
	scansDir    string
	broadcaster *Broadcaster
}

// NewRunner constructs a fleet runner. If broadcaster is nil, events are
// only written to disk (no live fan-out — useful for the CLI path).
func NewRunner(store *Store, scansDir string, broadcaster *Broadcaster) *Runner {
	return &Runner{store: store, scansDir: scansDir, broadcaster: broadcaster}
}

// StartScan matches api.StartScanFunc's signature shape but is package-local
// to keep the import graph one-directional (fleet does not import api). The
// CLI/HTTP callers adapt their concrete StartScanFunc into this closure.
type StartScan func(ctx context.Context, scanID, target string, offline bool, since string)

// Run blocks until every member's scan has terminated. parallel is the
// concurrent-scan cap (subscription-quota friendliness). Returns the final
// Snapshot.
//
// Critical: this routine spawns one goroutine to TailEvents per member.
// Those goroutines drain into the event sink AND remain alive until the
// member's terminal `done` event is read (or ctx is canceled). The worker
// goroutines themselves call into StartScan synchronously — so a worker
// that finishes its scan releases its semaphore slot for the next target.
func (r *Runner) Run(ctx context.Context, fleetID string, members []Member, parallel int, offline bool, start StartScan) (*Report, error) {
	if parallel <= 0 {
		parallel = 2
	}

	// Tailers follow each member's events.jsonl until its terminal `done`
	// event OR until their context is canceled. A member whose scan
	// subprocess dies WITHOUT writing `done` would otherwise loop forever —
	// and because the parent ctx is frequently context.Background(), so would
	// tailWG.Wait() below, wedging the whole fleet (only kill -9 recovers).
	// Give the tailers their own cancelable context and force-cancel it a
	// short grace period after all workers finish.
	tailCtx, cancelTails := context.WithCancel(ctx)
	defer cancelTails()

	// Per-member event tailers — start them BEFORE the workers spawn scans
	// so we never miss the first event. Each tailer pumps into our sink.
	var tailWG sync.WaitGroup
	for _, m := range members {
		m := m
		tailWG.Add(1)
		go func() {
			defer tailWG.Done()
			scanDir := filepath.Join(r.scansDir, assaymcp.DeriveTargetName(m.Target), m.ScanID)
			for ev := range assaymcp.TailEvents(tailCtx, scanDir) {
				r.onMemberEvent(fleetID, m.ScanID, ev)
			}
		}()
	}

	// Worker pool — semaphore caps concurrent scans at `parallel`.
	sem := make(chan struct{}, parallel)
	var workWG sync.WaitGroup
	for _, m := range members {
		m := m
		workWG.Add(1)
		sem <- struct{}{}
		go func() {
			defer workWG.Done()
			defer func() { <-sem }()
			// Always invoke start, even if ctx is already canceled. StartScan
			// is contractually required to emit a terminal event and call
			// runner.Complete on every path; skipping it on cancel would leave
			// this member's SSE subscribers (api.ScanRunner channel) hanging
			// forever. With a canceled ctx, StartScan fails fast and completes.
			start(ctx, m.ScanID, m.Target, offline, "")
		}()
	}
	workWG.Wait()

	// Workers finished, so every member's events.jsonl is fully written.
	// Tailers for healthy members exit on their `done` event; force-cancel
	// after a grace window so a member that never wrote `done` cannot block
	// tailWG.Wait() indefinitely.
	go func() {
		t := time.NewTimer(tailDrainGrace)
		defer t.Stop()
		select {
		case <-t.C:
		case <-ctx.Done():
		}
		cancelTails()
	}()
	tailWG.Wait()

	rep, err := r.store.Snapshot(fleetID, r.scansDir)
	if err != nil {
		return nil, err
	}
	rep.Status = StatusComplete
	if err := r.store.WriteReport(fleetID, rep); err != nil {
		return rep, err
	}
	if err := r.store.SetStatus(fleetID, StatusComplete); err != nil {
		return rep, err
	}
	if r.broadcaster != nil {
		// Final synthetic event so SSE clients know the fleet is done.
		r.broadcaster.Publish(Event{ScanID: "", Stage: "fleet", Status: "complete"})
	}
	return rep, nil
}

func (r *Runner) onMemberEvent(fleetID, scanID string, ev assaymcp.ProgressEvent) {
	_ = r.store.AppendEvent(fleetID, scanID, ev) // best-effort
	if r.broadcaster != nil {
		r.broadcaster.Publish(Event{
			ScanID:  scanID,
			Stage:   ev.Stage,
			Status:  ev.Status,
			Message: ev.Message,
			At:      ev.At,
		})
	}
}
