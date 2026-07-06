package fleet

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	assaymcp "github.com/chawdamrunal/assay/internal/mcp"
)

// mcpEvent is a tiny test-only helper to build assaymcp.ProgressEvent inline.
func mcpEvent(stage, status, msg string) assaymcp.ProgressEvent {
	return assaymcp.ProgressEvent{Stage: stage, Status: status, Message: msg}
}

func TestStoreAllocateAndLoadMeta(t *testing.T) {
	store := NewStore(t.TempDir())
	members := []Member{
		{Target: "/abs/path/a", ScanID: "scan-a"},
		{Target: "/abs/path/b", ScanID: "scan-b"},
	}
	dir, err := store.Allocate("fleet-1", members, []string{"skipped"})
	require.NoError(t, err)
	assert.DirExists(t, dir)

	meta, err := store.LoadMeta("fleet-1")
	require.NoError(t, err)
	assert.Equal(t, "fleet-1", meta.FleetID)
	assert.Equal(t, StatusRunning, meta.Status)
	assert.Len(t, meta.Members, 2)
	assert.Equal(t, []string{"skipped"}, meta.Excludes)
	assert.NotEmpty(t, meta.StartedAt)
}

// TestRunnerCompletesWhenMemberWritesNoDoneEvent regression-guards the fleet
// deadlock: a member whose scan crashes before writing its terminal `done`
// event must not wedge Runner.Run forever. Before the fix, the per-member
// tailer (reading on context.Background()) looped indefinitely and
// tailWG.Wait() never returned, requiring kill -9 of assay serve.
func TestRunnerCompletesWhenMemberWritesNoDoneEvent(t *testing.T) {
	scansDir := t.TempDir()
	store := NewStore(t.TempDir())

	members := []Member{{Target: "/abs/plug-a", ScanID: "scan-a"}}
	_, err := store.Allocate("fleet-x", members, nil)
	require.NoError(t, err)

	// Simulate a member scan that emits a couple of events then CRASHES
	// without ever writing the terminal {stage:"done"} event.
	start := func(_ context.Context, scanID, target string, _ bool, _ string) {
		scanDir := filepath.Join(scansDir, filepath.Base(target), scanID)
		require.NoError(t, os.MkdirAll(scanDir, 0o750))
		f, err := os.Create(filepath.Join(scanDir, "events.jsonl"))
		require.NoError(t, err)
		defer func() { _ = f.Close() }()
		for _, ev := range []assaymcp.ProgressEvent{
			mcpEvent("triage", "start", ""),
			mcpEvent("triage", "complete", "mapped"), // no "done" — crashed.
		} {
			line, _ := json.Marshal(ev)
			_, _ = f.Write(append(line, '\n'))
		}
	}

	runner := NewRunner(store, scansDir, nil)
	done := make(chan struct{})
	go func() {
		// context.Background() mirrors the real caller in api/fleet.go.
		_, _ = runner.Run(context.Background(), "fleet-x", members, 1, false, start)
		close(done)
	}()

	select {
	case <-done:
		// Run returned despite the missing terminal event — fix works.
	case <-time.After(10 * time.Second):
		t.Fatal("Runner.Run hung waiting for a member's terminal `done` event (deadlock regression)")
	}
}

func TestStoreRejectsBadFleetID(t *testing.T) {
	store := NewStore(t.TempDir())
	_, err := store.Allocate("../escape", nil, nil)
	require.Error(t, err)
	_, err = store.Allocate("", nil, nil)
	require.Error(t, err)
}

// TestLoadMetaReturnsErrFleetNotFound regression-guards the v0.5.1 fix
// for the bug where every "fleet not found" returned HTTP 500 because the
// handler compared against `fmt.Errorf("not found")` which never matches.
func TestLoadMetaReturnsErrFleetNotFound(t *testing.T) {
	store := NewStore(t.TempDir())
	_, err := store.LoadMeta("does-not-exist")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrFleetNotFound),
		"missing fleet must wrap ErrFleetNotFound so the API handler can return 404; got %v", err)
}

func TestSnapshotReturnsErrFleetNotFound(t *testing.T) {
	store := NewStore(t.TempDir())
	_, err := store.Snapshot("does-not-exist", t.TempDir())
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrFleetNotFound))
}

func TestSnapshotAggregatesCompletedFailedPendingMembers(t *testing.T) {
	scansDir := t.TempDir()
	fleetDir := t.TempDir()
	store := NewStore(fleetDir)

	// 1. complete member with one critical + one medium finding
	completeDir := filepath.Join(scansDir, "alpha", "scan-complete")
	require.NoError(t, os.MkdirAll(completeDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(completeDir, "audit.json"), []byte(`{
		"schema_version": "0.1",
		"scan_id": "scan-complete",
		"target": {"kind":"claude-code-plugin","name":"alpha"},
		"scanned_at": "2026-05-01T00:00:00Z",
		"scanner": {"name":"assay","version":"0.4","model":"x","prompt_version":"mcp-v2"},
		"verdict": "unsafe",
		"findings": [
			{"id":"F1","severity":"critical","category":"exfil","title":"X","evidence":[{"file":"f","line":1,"snippet":"."}]},
			{"id":"F2","severity":"medium","category":"overscope","title":"Y","evidence":[{"file":"f","line":2,"snippet":"."}]}
		]
	}`), 0o600))

	// 2. failed member with error.json
	failedDir := filepath.Join(scansDir, "beta", "scan-failed")
	require.NoError(t, os.MkdirAll(failedDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(failedDir, "error.json"), []byte(`{"error":"rate limited"}`), 0o600))

	// 3. pending member: dir exists but no audit.json, no error.json
	require.NoError(t, os.MkdirAll(filepath.Join(scansDir, "gamma", "scan-pending"), 0o750))

	_, err := store.Allocate("f1", []Member{
		{Target: "/p/alpha", ScanID: "scan-complete"},
		{Target: "/p/beta", ScanID: "scan-failed"},
		{Target: "/p/gamma", ScanID: "scan-pending"},
	}, nil)
	require.NoError(t, err)

	rep, err := store.Snapshot("f1", scansDir)
	require.NoError(t, err)
	require.Len(t, rep.Members, 3)

	byID := map[string]MemberReport{}
	for _, m := range rep.Members {
		byID[m.ScanID] = m
	}
	assert.Equal(t, "complete", byID["scan-complete"].Status)
	assert.Equal(t, "unsafe", byID["scan-complete"].Verdict)
	assert.Equal(t, 2, byID["scan-complete"].Findings)
	assert.Equal(t, 1, byID["scan-complete"].Critical)
	assert.Equal(t, 1, byID["scan-complete"].Medium)

	assert.Equal(t, "failed", byID["scan-failed"].Status)
	assert.Contains(t, byID["scan-failed"].ErrorReason, "rate limited")

	assert.Equal(t, "pending", byID["scan-pending"].Status)

	// Aggregate counters
	assert.Equal(t, 1, rep.Verdict.Unsafe)
	assert.Equal(t, 1, rep.Severity.Critical)
	assert.Equal(t, 1, rep.Severity.Medium)

	// Pending member means overall fleet is not yet complete.
	assert.Equal(t, StatusRunning, rep.Status)
}

func TestSnapshotMarksCompleteWhenAllTerminal(t *testing.T) {
	scansDir := t.TempDir()
	store := NewStore(t.TempDir())
	dir := filepath.Join(scansDir, "p", "s1")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "audit.json"), []byte(`{
		"schema_version":"0.1","scan_id":"s1","target":{"kind":"x","name":"p"},
		"scanned_at":"2026-01-01T00:00:00Z","scanner":{"name":"assay","version":"0","model":"x","prompt_version":"x"},
		"verdict":"safe","findings":[]
	}`), 0o600))
	_, err := store.Allocate("f", []Member{{Target: "/p/p", ScanID: "s1"}}, nil)
	require.NoError(t, err)

	rep, err := store.Snapshot("f", scansDir)
	require.NoError(t, err)
	assert.Equal(t, StatusComplete, rep.Status)
	assert.NotEmpty(t, rep.FinishedAt)
}

func TestAppendEventCreatesJSONL(t *testing.T) {
	fleetDir := t.TempDir()
	store := NewStore(fleetDir)
	_, err := store.Allocate("f", nil, nil)
	require.NoError(t, err)

	// Using the package's own type alias for the event would require an
	// import-cycle workaround; tests construct via mcp directly.
	// We sidestep by writing through the helper and reading back the line.
	require.NoError(t, store.AppendEvent("f", "scan-a", mcpEvent("triage", "complete", "")))
	require.NoError(t, store.AppendEvent("f", "scan-a", mcpEvent("done", "complete", "safe")))

	data, err := os.ReadFile(filepath.Join(fleetDir, "f", "events.jsonl"))
	require.NoError(t, err)
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			if i > start {
				lines = append(lines, data[start:i])
			}
			start = i + 1
		}
	}
	require.Len(t, lines, 2)
	var first map[string]any
	require.NoError(t, json.Unmarshal(lines[0], &first))
	assert.Equal(t, "scan-a", first["scan_id"])
	assert.Equal(t, "triage", first["stage"])
}

func TestListReturnsRecentFirst(t *testing.T) {
	store := NewStore(t.TempDir())
	_, _ = store.Allocate("aaa", nil, nil)
	_, _ = store.Allocate("bbb", nil, nil)
	_, _ = store.Allocate("ccc", nil, nil)
	got, err := store.List()
	require.NoError(t, err)
	require.Len(t, got, 3)
	// They were created in order — sorted descending by started_at, so the
	// last one allocated should be first.
	assert.Equal(t, "ccc", got[0].FleetID)
}

func TestBroadcasterFanOutNonBlocking(t *testing.T) {
	b := NewBroadcaster()
	ch1, unsub1 := b.Subscribe(2)
	ch2, unsub2 := b.Subscribe(2)
	defer unsub1()
	defer unsub2()
	b.Publish(Event{ScanID: "s1", Stage: "triage", Status: "start"})
	b.Publish(Event{ScanID: "s1", Stage: "triage", Status: "complete"})

	collect := func(ch <-chan Event, n int) []Event {
		out := []Event{}
		for i := 0; i < n; i++ {
			out = append(out, <-ch)
		}
		return out
	}
	g1 := collect(ch1, 2)
	g2 := collect(ch2, 2)
	assert.Equal(t, g1, g2, "both subscribers receive same stream")
	assert.Equal(t, "complete", g1[1].Status)
}

func TestBroadcasterCloseClosesAllChannels(t *testing.T) {
	b := NewBroadcaster()
	ch1, _ := b.Subscribe(1)
	ch2, _ := b.Subscribe(1)
	b.Close()
	_, ok1 := <-ch1
	_, ok2 := <-ch2
	assert.False(t, ok1)
	assert.False(t, ok2)
}
