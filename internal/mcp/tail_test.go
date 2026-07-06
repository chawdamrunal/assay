package mcp

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTailEventsWaitsForFileCreation covers the realistic order: the tailer
// starts BEFORE the MCP subprocess has called assay_scan_start, which is
// what creates events.jsonl. The tailer must poll for the file to appear
// instead of dying on ENOENT.
func TestTailEventsWaitsForFileCreation(t *testing.T) {
	scanDir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ch := TailEvents(ctx, scanDir)

	// Create the file after a small delay.
	time.Sleep(200 * time.Millisecond)
	require.NoError(t, os.WriteFile(
		filepath.Join(scanDir, "events.jsonl"),
		[]byte(`{"stage":"triage","status":"start"}`+"\n"+`{"stage":"done","status":"complete"}`+"\n"),
		0o600,
	))

	var got []ProgressEvent
	for ev := range ch {
		got = append(got, ev)
	}
	require.Len(t, got, 2)
	assert.Equal(t, "triage", got[0].Stage)
	assert.Equal(t, "done", got[1].Stage)
}

// TestTailEventsFollowsAppends asserts the tailer notices new lines appended
// after the file already exists — the model the MCP subprocess uses across
// multiple emit_progress calls.
func TestTailEventsFollowsAppends(t *testing.T) {
	scanDir := t.TempDir()
	evPath := filepath.Join(scanDir, "events.jsonl")
	require.NoError(t, os.WriteFile(evPath, []byte(`{"stage":"prepass","status":"start"}`+"\n"), 0o600))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ch := TailEvents(ctx, scanDir)

	// Append more events after a delay.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(300 * time.Millisecond)
		f, err := os.OpenFile(evPath, os.O_APPEND|os.O_WRONLY, 0o600)
		require.NoError(t, err)
		_, _ = f.WriteString(`{"stage":"triage","status":"complete"}` + "\n")
		_, _ = f.WriteString(`{"stage":"done","status":"complete","message":"safe"}` + "\n")
		_ = f.Close()
	}()

	var got []ProgressEvent
	for ev := range ch {
		got = append(got, ev)
	}
	wg.Wait()
	require.GreaterOrEqual(t, len(got), 3)
	assert.Equal(t, "done", got[len(got)-1].Stage)
	assert.Equal(t, "safe", got[len(got)-1].Message)
}

func TestTailEventsCancelStops(t *testing.T) {
	scanDir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	ch := TailEvents(ctx, scanDir)
	cancel()
	// Drain — channel must close even though events.jsonl was never created.
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case _, ok := <-ch:
		assert.False(t, ok, "channel should close on cancel")
	case <-timer.C:
		t.Fatal("tail goroutine did not exit on cancel")
	}
}
