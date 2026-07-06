package api

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chawdamrunal/assay/internal/scanner"
)

func TestScanRunnerRegisterAndStream(t *testing.T) {
	sr := NewScanRunner()

	scanID := "test-scan-1"
	events := sr.Register(scanID)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		sr.Emit(scanID, scanner.Event{Stage: "triage", Status: "start"})
		sr.Emit(scanID, scanner.Event{Stage: "triage", Status: "complete"})
		sr.Complete(scanID)
	}()

	var got []scanner.Event
	deadline := time.After(2 * time.Second)
loop:
	for {
		select {
		case e, ok := <-events:
			if !ok {
				break loop
			}
			got = append(got, e)
		case <-deadline:
			t.Fatal("timed out waiting for events")
		}
	}
	wg.Wait()

	require.Len(t, got, 2)
	assert.Equal(t, "triage", got[0].Stage)
	assert.Equal(t, "complete", got[1].Status)
}

// TestScanRunnerCompletePurgesActiveMap regression-guards the unbounded
// memory growth bug: Complete must delete the scan's entry from the active
// map, not merely mark it closed. Without the delete, a long-lived serve
// accumulates one scanState per scan forever.
func TestScanRunnerCompletePurgesActiveMap(t *testing.T) {
	sr := NewScanRunner()
	sr.Register("scan-a")
	sr.Register("scan-b")

	sr.mu.Lock()
	require.Len(t, sr.active, 2)
	sr.mu.Unlock()

	sr.Complete("scan-a")

	sr.mu.Lock()
	_, stillThere := sr.active["scan-a"]
	got := len(sr.active)
	sr.mu.Unlock()

	assert.False(t, stillThere, "completed scan must be removed from active map")
	assert.Equal(t, 1, got, "active map should shrink on Complete")
	assert.False(t, sr.IsActive("scan-a"))
	assert.True(t, sr.IsActive("scan-b"))
}

func TestScanRunnerStatus(t *testing.T) {
	sr := NewScanRunner()
	sr.Register("s1")

	assert.True(t, sr.IsActive("s1"))
	assert.False(t, sr.IsActive("nonexistent"))

	sr.Complete("s1")
	assert.False(t, sr.IsActive("s1"))
}

func TestScanRunnerDropsLateSubscribers(t *testing.T) {
	sr := NewScanRunner()
	sr.Register("s1")
	sr.Complete("s1")

	// Subscribing after Complete returns nil.
	assert.Nil(t, sr.Subscribe("s1"))
}

// TestScanRunnerBroadcastsToEverySubscriber regression-guards the v0.6 fix
// for the bug where multiple SSE subscribers shared a single channel and
// load-balanced events between themselves. The TopBar progress indicator
// and the LiveScanPage both subscribe to the same scan; each must receive
// every event.
func TestScanRunnerBroadcastsToEverySubscriber(t *testing.T) {
	sr := NewScanRunner()
	sr.Register("multi-scan")

	subA := sr.Subscribe("multi-scan")
	subB := sr.Subscribe("multi-scan")
	require.NotNil(t, subA)
	require.NotNil(t, subB)

	const n = 5
	go func() {
		for i := 0; i < n; i++ {
			sr.Emit("multi-scan", scanner.Event{Stage: "prepass", Status: "start"})
		}
		sr.Complete("multi-scan")
	}()

	countA, countB := 0, 0
	for e := range subA {
		_ = e
		countA++
	}
	for e := range subB {
		_ = e
		countB++
	}
	assert.Equal(t, n, countA, "subscriber A should receive every event")
	assert.Equal(t, n, countB, "subscriber B should receive every event")
}

// TestScanRunnerUnsubscribeRemovesOneSlot guards Unsubscribe so a flapping
// SSE client doesn't accumulate stale subscriber channels.
func TestScanRunnerUnsubscribeRemovesOneSlot(t *testing.T) {
	sr := NewScanRunner()
	sr.Register("u-scan")
	subA := sr.Subscribe("u-scan")
	subB := sr.Subscribe("u-scan")
	require.NotNil(t, subA)
	require.NotNil(t, subB)

	sr.Unsubscribe("u-scan", subA)

	go func() {
		sr.Emit("u-scan", scanner.Event{Stage: "triage", Status: "start"})
		sr.Complete("u-scan")
	}()

	// subA was unsubscribed → its channel should already be closed and
	// yield zero events from the post-Unsubscribe Emit.
	countA := 0
	for range subA {
		countA++
	}
	countB := 0
	for range subB {
		countB++
	}
	assert.Equal(t, 0, countA, "unsubscribed channel should not receive new events")
	assert.Equal(t, 1, countB, "remaining subscriber should still get the event")
}

// TestScanRunnerEmitCompleteRace fires many Emit + Complete pairs from
// competing goroutines. Before the v0.5.1 fix this reliably panicked with
// "send on closed channel" under `go test -race` because Emit released the
// mutex before its select, allowing Complete to close the channel mid-send.
// The select{default} branch does NOT save you from sending on a closed
// channel — only mutual exclusion does.
func TestScanRunnerEmitCompleteRace(t *testing.T) {
	const iterations = 200
	for i := 0; i < iterations; i++ {
		sr := NewScanRunner()
		scanID := "race-scan"
		events := sr.Register(scanID)

		var wg sync.WaitGroup
		// Drainer — eat events as fast as we can so the buffer doesn't fill.
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range events {
			}
		}()
		// Producer — emit forever until Complete closes the channel.
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				sr.Emit(scanID, scanner.Event{Stage: "triage", Status: "start"})
			}
		}()
		// Closer — fires almost immediately.
		wg.Add(1)
		go func() {
			defer wg.Done()
			sr.Complete(scanID)
		}()
		wg.Wait()
	}
}

// Smoke: emit-then-complete order doesn't deadlock the runner.
func TestScanRunnerNoDeadlockOnCompleteBeforeSubscriber(t *testing.T) {
	sr := NewScanRunner()
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		sr.Register("s2")
		sr.Emit("s2", scanner.Event{Stage: "done", Status: "complete"})
		sr.Complete("s2")
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("scan runner deadlocked")
	}
}
