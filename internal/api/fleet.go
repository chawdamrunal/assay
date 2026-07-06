package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/chawdamrunal/assay/internal/fleet"
	"github.com/chawdamrunal/assay/internal/inventory"
)

// FleetDeps is the dependency bundle the fleet handler needs.
type FleetDeps struct {
	// FleetDir is ~/.assay/fleet — the root under which Allocate creates
	// <fleet_id>/ directories.
	FleetDir string
	// ScansDir is ~/.assay/scans — shared with the regular scan handlers.
	ScansDir string
	// LoadInventory enumerates installed plugins (and MCP servers, hooks,
	// settings — fleet filters to plugins only). Reuses the same loader the
	// /api/inventory endpoint uses.
	LoadInventory InventoryLoader
	// StartScan delegates per-member execution to the same closure
	// /api/scans uses (typically buildMCPStartScan). Fleet drives concurrent
	// scans by invoking this N times; the StartScan implementation handles
	// allocate-dir + spawn-claude-p as usual.
	StartScan StartScanFunc
	// Runner is shared with /api/scans/:id/stream so a fleet scan's
	// per-member SSE streams stay reachable via the existing endpoint. Each
	// fleet member also has its own scan_id, so a user who wants to follow
	// one specific member can navigate to /scans/live/<scan_id> as usual.
	Runner *ScanRunner
	// ServerCtx is the server-lifetime context. The fleet runner goroutine
	// derives from it (not context.Background()) so SIGTERM cancels in-flight
	// fleet scans cleanly. Nil falls back to context.Background().
	ServerCtx context.Context
}

// maxFleetParallel caps how many member scans run concurrently regardless of
// the requested value — an unbounded `parallel` could fan out dozens of
// `claude` subprocesses and exhaust the host.
const maxFleetParallel = 8

// fleetSession tracks one in-flight fleet by ID. Holds the broadcaster so
// /api/fleet/:id/stream can subscribe; the runner closes the broadcaster
// when the fleet finishes.
type fleetSession struct {
	broadcaster *fleet.Broadcaster
	done        chan struct{}
}

// fleetRegistry is the in-process index of running fleet sessions. SSE
// subscribers look up by fleet_id; the runner goroutine removes the entry
// when the fleet completes. Past fleets without an entry are read from disk.
type fleetRegistry struct {
	mu       sync.RWMutex
	sessions map[string]*fleetSession
}

func newFleetRegistry() *fleetRegistry { return &fleetRegistry{sessions: map[string]*fleetSession{}} }

func (r *fleetRegistry) put(id string, s *fleetSession) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[id] = s
}

func (r *fleetRegistry) get(id string) (*fleetSession, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sessions[id]
	return s, ok
}

func (r *fleetRegistry) drop(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessions, id)
}

// NewFleetHandler returns the http.Handler that routes /api/fleet*.
//
// Routes:
//
//	POST /api/fleet/scan        — start a new fleet scan; returns {fleet_id, members:[...]}
//	GET  /api/fleet             — list past fleets (most recent first)
//	GET  /api/fleet/:id         — snapshot + meta of one fleet
//	GET  /api/fleet/:id/stream  — SSE stream of per-member events
//
// Path routing is done inline because the existing server uses a simple
// ServeMux setup, not chi/gin/etc. Mirrors the pattern in scans.go.
func NewFleetHandler(deps FleetDeps) http.Handler {
	store := fleet.NewStore(deps.FleetDir)
	registry := newFleetRegistry()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/fleet")
		path = strings.TrimPrefix(path, "/")
		switch {
		case (path == "" || path == "/") && r.Method == http.MethodGet:
			handleFleetList(store, w, r)
		case path == "scan" && r.Method == http.MethodPost:
			handleFleetStart(deps, store, registry, w, r)
		case strings.HasSuffix(path, "/stream") && r.Method == http.MethodGet:
			handleFleetStream(deps, registry, strings.TrimSuffix(path, "/stream"), w, r)
		case path != "" && r.Method == http.MethodGet:
			handleFleetGet(deps, store, path, w, r)
		default:
			WriteJSONError(w, http.StatusMethodNotAllowed, "method not allowed for /api/fleet")
		}
	})
}

// FleetStartRequest is the POST /api/fleet/scan body.
type FleetStartRequest struct {
	Exclude  []string `json:"exclude,omitempty"`
	Parallel int      `json:"parallel,omitempty"`
	Quick    bool     `json:"quick,omitempty"`
}

// FleetStartResponse is returned by POST /api/fleet/scan.
type FleetStartResponse struct {
	FleetID string         `json:"fleet_id"`
	Members []fleet.Member `json:"members"`
}

func handleFleetStart(deps FleetDeps, store *fleet.Store, registry *fleetRegistry, w http.ResponseWriter, r *http.Request) {
	if deps.StartScan == nil {
		WriteJSONError(w, http.StatusNotImplemented, "fleet scans not wired (StartScan dep is nil)")
		return
	}
	var req FleetStartRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	if req.Parallel <= 0 {
		req.Parallel = 2
	}
	if req.Parallel > maxFleetParallel {
		req.Parallel = maxFleetParallel
	}
	excludeSet := map[string]bool{}
	for _, e := range req.Exclude {
		excludeSet[e] = true
	}

	inv, err := deps.LoadInventory()
	if err != nil {
		WriteJSONError(w, http.StatusInternalServerError, "load inventory: "+err.Error())
		return
	}
	members := []fleet.Member{}
	for _, it := range inv.Items {
		if it.Kind != inventory.KindClaudeCodePlugin {
			continue
		}
		if it.LocalPath == "" || excludeSet[it.Name] {
			continue
		}
		members = append(members, fleet.Member{Target: it.LocalPath, ScanID: uuid.NewString()})
	}
	if len(members) == 0 {
		WriteJSONError(w, http.StatusBadRequest, "no plugins to scan (inventory empty or all excluded)")
		return
	}

	fleetID := uuid.NewString()
	if _, err := store.Allocate(fleetID, members, req.Exclude); err != nil {
		WriteJSONError(w, http.StatusInternalServerError, "allocate fleet: "+err.Error())
		return
	}

	broadcaster := fleet.NewBroadcaster()
	session := &fleetSession{broadcaster: broadcaster, done: make(chan struct{})}
	registry.put(fleetID, session)

	// Register every member with the existing ScanRunner so per-member SSE
	// endpoints (/api/scans/:id/stream) work too. Each member also drives
	// its own buildMCPStartScan invocation in the goroutine below.
	for _, m := range members {
		deps.Runner.Register(m.ScanID)
	}

	// Drive the fleet in a background goroutine. We DO NOT wait — the POST
	// returns 202 immediately with the fleet_id.
	go func() {
		defer close(session.done)
		defer registry.drop(fleetID)
		// Derive from the server-lifetime context so SIGTERM cancels the fleet
		// runner (and its member scans) instead of orphaning the goroutine for
		// the supervisor to SIGKILL mid-write. Background for tests/legacy.
		ctx := deps.ServerCtx
		if ctx == nil {
			ctx = context.Background()
		}
		runner := fleet.NewRunner(store, deps.ScansDir, broadcaster)
		start := func(ctx context.Context, scanID, target string, offline bool, since string) {
			// Reuse the same StartScan path the single-scan endpoint uses —
			// includes spawn-claude, error.json on failure, auto-diff if since
			// is set. Fleet doesn't auto-diff (since="").
			deps.StartScan(ctx, scanID, target, offline, since, deps.Runner)
		}
		_, _ = runner.Run(ctx, fleetID, members, req.Parallel, false, start)
		broadcaster.Close()
	}()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(FleetStartResponse{FleetID: fleetID, Members: members})
}

func handleFleetList(store *fleet.Store, w http.ResponseWriter, _ *http.Request) {
	metas, err := store.List()
	if err != nil {
		WriteJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if metas == nil {
		metas = []fleet.Meta{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"items": metas})
}

func handleFleetGet(deps FleetDeps, store *fleet.Store, fleetID string, w http.ResponseWriter, _ *http.Request) {
	if !fleet.ValidID(fleetID) {
		WriteJSONError(w, http.StatusBadRequest, "invalid fleet id")
		return
	}
	// Prefer the persisted report.json (written when the fleet finished) so a
	// completed fleet returns its real finished_at + aggregate. Fall back to a
	// live Snapshot for in-flight fleets, whose report.json isn't written yet.
	if rep, err := store.LoadReport(fleetID); err == nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rep)
		return
	}
	rep, err := store.Snapshot(fleetID, deps.ScansDir)
	if err != nil {
		if errors.Is(err, fleet.ErrFleetNotFound) {
			WriteJSONError(w, http.StatusNotFound, "fleet not found: "+fleetID)
			return
		}
		WriteJSONError(w, http.StatusInternalServerError, "snapshot: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rep)
}

func handleFleetStream(deps FleetDeps, registry *fleetRegistry, fleetID string, w http.ResponseWriter, r *http.Request) {
	// Validate before the raw id reaches the 404 body or disk-path construction.
	if !fleet.ValidID(fleetID) {
		WriteJSONError(w, http.StatusBadRequest, "invalid fleet id")
		return
	}
	if _, ok := w.(http.Flusher); !ok {
		WriteJSONError(w, http.StatusInternalServerError, "streaming not supported by this server/proxy")
		return
	}
	session, ok := registry.get(fleetID)
	if !ok {
		// No live session — the fleet already finished (the runner drops its
		// registry entry on exit). Replay events.jsonl from disk so a
		// post-completion page load still populates the timeline, then close.
		// Without this the FE EventSource sees a 404 (non-SSE) and the
		// timeline never renders for completed fleets.
		sse := NewSSEWriter(w)
		for _, ev := range loadFleetEventsFromDisk(deps.FleetDir, fleetID) {
			_ = sse.WriteEvent(ev.Stage, ev)
		}
		_ = sse.WriteEvent("done", map[string]string{"fleet_id": fleetID})
		return
	}
	ch, unsub := session.broadcaster.Subscribe(64)
	defer unsub()

	sse := NewSSEWriter(w)
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				_ = sse.WriteEvent("done", map[string]string{"fleet_id": fleetID})
				return
			}
			_ = sse.WriteEvent(ev.Stage, ev)
		case <-session.done:
			_ = sse.WriteEvent("done", map[string]string{"fleet_id": fleetID})
			return
		}
	}
}

// loadFleetEventsFromDisk reads a completed fleet's events.jsonl and returns
// every parsed Event in order. Best-effort: returns nil on any error so
// the stream handler degrades to just emitting "done".
func loadFleetEventsFromDisk(fleetDir, fleetID string) []fleet.Event {
	if fleetDir == "" || !fleet.ValidID(fleetID) {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(fleetDir, fleetID, "events.jsonl")) // #nosec G304 -- fleetID validated, fleetDir-bounded
	if err != nil {
		return nil
	}
	out := make([]fleet.Event, 0, 32)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ev fleet.Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		out = append(out, ev)
	}
	return out
}
