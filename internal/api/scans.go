package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/chawdamrunal/assay/internal/claude"
	"github.com/chawdamrunal/assay/internal/scanner"
	"github.com/chawdamrunal/assay/internal/verdict"
)

// ScanRequest is the POST /api/scans body.
type ScanRequest struct {
	Target  string `json:"target"`
	Offline bool   `json:"offline,omitempty"`
	// Since opts the scan into auto-diff against a prior scan. Values:
	//   ""        — no diff
	//   "latest"  — diff against most-recent prior scan of the same target
	//   "<id>"    — diff against the named scan_id
	Since string `json:"since,omitempty"`
}

// ScanResponse is the POST /api/scans response.
type ScanResponse struct {
	ScanID string `json:"scan_id"`
}

// StartScanFunc runs a scan in the background and emits events to runner.
// Implementations MUST call runner.Complete(scanID) on every exit path
// (success or error) so SSE subscribers don't hang.
//
// `since` is the auto-diff baseline reference; empty disables auto-diff.
// Implementations should annotate the final audit.json with diff information
// when set (see internal/mcp/prior_scan.go + verdict.Diff).
type StartScanFunc func(ctx context.Context, scanID, target string, offline bool, since string, runner *ScanRunner)

// ScansDeps are the dependencies the scans handlers need.
type ScansDeps struct {
	ScansDir  string
	Runner    *ScanRunner
	StartScan StartScanFunc // injected so tests can use FakeClient
	// AllowedRoots restricts which filesystem paths a POST /api/scans
	// request can target. Empty disables the guard (only used by tests
	// that pre-validate their inputs); production callers must populate
	// this with DefaultAllowedRoots(claudeDir) plus any workspace dirs.
	AllowedRoots []string
	// ServerCtx is the server-lifetime context. Background scan goroutines
	// derive from it (not context.Background()) so SIGTERM/shutdown cancels
	// in-flight scans cleanly instead of leaving them orphaned for the
	// supervisor to SIGKILL mid-write. Nil falls back to context.Background().
	ServerCtx context.Context
}

// NewScansHandler routes under /api/scans. With deps, it implements:
//
//	GET    /api/scans                  -> list of completed scans
//	POST   /api/scans                  -> start a scan, return {scan_id}
//	GET    /api/scans/:id              -> the audit.json for that scan (404 if not found)
//	GET    /api/scans/:id/stream       -> SSE stream of events for an active scan
//
// If deps is the zero value (e.g., test wires a placeholder), the handler returns
// the original empty/501 placeholders — useful for the Plan 3 baseline tests.
func NewScansHandler(scansDirOrDeps any) http.Handler {
	switch v := scansDirOrDeps.(type) {
	case string:
		return newPlaceholderScansHandler(v)
	case ScansDeps:
		return newRealScansHandler(v)
	default:
		return http.NotFoundHandler()
	}
}

// newPlaceholderScansHandler preserves the Plan 3 surface: GET → empty, POST → 501.
// Plan 3's existing tests call NewScansHandler(scansDir string).
func newPlaceholderScansHandler(scansDir string) http.Handler {
	_ = scansDir
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/scans")
		path = strings.TrimPrefix(path, "/")
		switch {
		case path == "" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
		case path == "" && r.Method == http.MethodPost:
			WriteJSONError(w, http.StatusNotImplemented, "scan execution arrives in Plan 2 (Scanner Brain)")
		case path != "" && r.Method == http.MethodGet:
			WriteJSONError(w, http.StatusNotFound, "scan not found: "+path)
		default:
			WriteJSONError(w, http.StatusMethodNotAllowed, "method not allowed for /api/scans")
		}
	})
}

// newRealScansHandler implements the full surface.
func newRealScansHandler(deps ScansDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/scans")
		path = strings.TrimPrefix(path, "/")

		switch {
		case path == "" && r.Method == http.MethodGet:
			handleListScans(deps, w, r)
		case path == "" && r.Method == http.MethodPost:
			handleStartScan(deps, w, r)
		case strings.HasSuffix(path, "/stream") && r.Method == http.MethodGet:
			id := strings.TrimSuffix(path, "/stream")
			handleStreamScan(deps, id, w, r)
		case path != "" && r.Method == http.MethodGet:
			handleGetScan(deps, path, w, r)
		case path != "" && r.Method == http.MethodDelete:
			handleDeleteScan(deps, path, w, r)
		default:
			WriteJSONError(w, http.StatusMethodNotAllowed, "method not allowed for /api/scans")
		}
	})
}

// abandonedAge is how old a "pending" scan dir has to be (modtime) before
// we hide it from the list. Long-running MCP scans finish in a few minutes;
// anything pending for > 1 hour is dead.
const abandonedAge = 1 * time.Hour

func handleListScans(deps ScansDeps, w http.ResponseWriter, _ *http.Request) {
	entries, err := os.ReadDir(deps.ScansDir)
	if err != nil && !os.IsNotExist(err) {
		WriteJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := []map[string]any{}
	now := time.Now()
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// Each target dir contains scan-id subdirs.
		targetDir := filepath.Join(deps.ScansDir, e.Name())
		scanDirs, _ := os.ReadDir(targetDir)
		for _, sd := range scanDirs {
			if !sd.IsDir() {
				continue
			}
			scanDirPath := filepath.Join(targetDir, sd.Name())
			status := scanDirStatus(scanDirPath)
			info, statErr := sd.Info()
			// Hide pending scans older than abandonedAge — those are dead
			// processes that left a meta.json but never produced audit.json
			// or error.json. They cluttered the UI with phantom rows that
			// 404 on the report page and never get cleaned.
			if status == "pending" && statErr == nil && now.Sub(info.ModTime()) > abandonedAge {
				continue
			}
			item := map[string]any{
				"scan_id": sd.Name(),
				"target":  e.Name(),
				"dir":     scanDirPath,
				"status":  status,
			}
			// Surface the verdict on the list so the Scans page can render a
			// safe/caution/unsafe badge without opening each report.
			if status == "complete" {
				if v := readScanVerdict(scanDirPath); v != "" {
					item["verdict"] = v
				}
			}
			if statErr == nil {
				item["created_at"] = info.ModTime().UTC().Format("2006-01-02T15:04:05.000Z")
			}
			items = append(items, item)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
}

// readScanVerdict reads just the top-level "verdict" field from a scan's
// audit.json. Best-effort: returns "" on any error so the list endpoint
// degrades to no-badge rather than failing.
func readScanVerdict(scanDir string) string {
	data, err := os.ReadFile(filepath.Join(scanDir, "audit.json")) // #nosec G304 -- scansDir-bounded
	if err != nil {
		return ""
	}
	var v struct {
		Verdict string `json:"verdict"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return ""
	}
	return v.Verdict
}

// scanDirStatus reports the on-disk status of a scan directory.
//   - "complete" if audit.json exists,
//   - "failed"   if error.json exists (written by the serve runner on failure),
//   - "pending"  otherwise (in-flight or never finalized).
func scanDirStatus(scanDir string) string {
	if _, err := os.Stat(filepath.Join(scanDir, "audit.json")); err == nil {
		return "complete"
	}
	if _, err := os.Stat(filepath.Join(scanDir, "error.json")); err == nil {
		return "failed"
	}
	return "pending"
}

func handleStartScan(deps ScansDeps, w http.ResponseWriter, r *http.Request) {
	var req ScanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteJSONError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if req.Target == "" {
		WriteJSONError(w, http.StatusBadRequest, "target is required")
		return
	}
	if deps.StartScan == nil {
		WriteJSONError(w, http.StatusNotImplemented, "scan execution not wired (StartScan dep is nil)")
		return
	}
	// Path-traversal guard: reject targets outside the configured allowed
	// roots so a cross-origin request can't read /etc/passwd through the
	// scanner. Disabled only when AllowedRoots is empty (test paths).
	cleanedTarget := req.Target
	if len(deps.AllowedRoots) > 0 {
		validated, err := EnsureAllowed(req.Target, deps.AllowedRoots)
		if err != nil {
			WriteJSONError(w, http.StatusForbidden, ErrPathNotAllowed.Error())
			return
		}
		cleanedTarget = validated
	}
	scanID := uuid.NewString()
	deps.Runner.Register(scanID)

	// The scan must outlive the POST request (the request context is canceled
	// the moment we return), but it must still be canceled on server shutdown
	// so SIGTERM doesn't leave it orphaned. Derive from the server-lifetime
	// context when wired; fall back to Background for tests/legacy callers.
	scanCtx := deps.ServerCtx
	if scanCtx == nil {
		scanCtx = context.Background()
	}
	go deps.StartScan(scanCtx, scanID, cleanedTarget, req.Offline, req.Since, deps.Runner)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(ScanResponse{ScanID: scanID})
}

func handleStreamScan(deps ScansDeps, scanID string, w http.ResponseWriter, r *http.Request) {
	// Validate the id before it reaches the runner lookup, the 404 body, or
	// findScanDirByID's path construction — every other scan handler does.
	if !safeScanID(scanID) {
		WriteJSONError(w, http.StatusBadRequest, "invalid scan id")
		return
	}
	// SSE needs a flushing ResponseWriter. Behind a non-flushing proxy
	// (buffering nginx, some ALBs) WriteEvent would silently no-op and the
	// client's EventSource would hang on an empty 200. Fail loudly instead.
	if _, ok := w.(http.Flusher); !ok {
		WriteJSONError(w, http.StatusInternalServerError, "streaming not supported by this server/proxy")
		return
	}
	ch := deps.Runner.Subscribe(scanID)
	if ch == nil {
		WriteJSONError(w, http.StatusNotFound, "scan not active: "+scanID)
		return
	}
	defer deps.Runner.Unsubscribe(scanID, ch)
	sse := NewSSEWriter(w)
	// Flush headers immediately so the FE EventSource considers the
	// connection open even when the first stage event is seconds away.
	_ = sse.WriteEvent("ping", map[string]string{"scan_id": scanID})

	// Replay any events already on disk before entering the live tail.
	// Late subscribers (React StrictMode double-mount, navigation back
	// to live page, page reload, post-restart reconnect) would otherwise
	// miss events that fired before they subscribed and the progress
	// chip would stay at 0% even though the scan is well underway.
	for _, replayed := range loadEventsFromDisk(deps.ScansDir, scanID) {
		_ = sse.WriteEvent(replayed.Stage, replayed)
	}

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-ch:
			if !ok {
				_ = sse.WriteEvent("done", map[string]string{"scan_id": scanID})
				return
			}
			_ = sse.WriteEvent(e.Stage, e)
		}
	}
}

// loadEventsFromDisk reads the scan's events.jsonl from the on-disk store
// (best-effort) and returns every parsed event in order. Errors fall back
// to an empty slice — replay is purely a UX nicety; if the file can't be
// read the live tail still works.
func loadEventsFromDisk(scansDir, scanID string) []scanner.Event {
	scanDir, err := findScanDirByID(scansDir, scanID)
	if err != nil || scanDir == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(scanDir, "events.jsonl")) // #nosec G304 -- scansDir-bound
	if err != nil {
		return nil
	}
	out := make([]scanner.Event, 0, 16)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ev scanner.Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		out = append(out, ev)
	}
	return out
}

// findScanDirByID walks scansDir for a child matching <basename>/<scanID>/.
// Returns the first match. Used by replay; tolerant of layout changes.
func findScanDirByID(scansDir, scanID string) (string, error) {
	entries, err := os.ReadDir(scansDir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		candidate := filepath.Join(scansDir, e.Name(), scanID)
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate, nil
		}
	}
	return "", nil
}

// safeScanID returns true if scanID is safe to use as a single path segment.
// Accepts UUIDs and CLI-allocated timestamp IDs like "20260515T055441.436Z".
func safeScanID(scanID string) bool {
	if scanID == "" || len(scanID) > 128 {
		return false
	}
	if strings.Contains(scanID, "..") {
		return false
	}
	for _, r := range scanID {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.':
		default:
			return false
		}
	}
	return true
}

func handleGetScan(deps ScansDeps, scanID string, w http.ResponseWriter, _ *http.Request) {
	if !safeScanID(scanID) {
		WriteJSONError(w, http.StatusBadRequest, "invalid scan id")
		return
	}
	// Find the scan by walking scansDir/*/scanID/. Prefer audit.json
	// (completed scan); fall back to error.json (failed scan).
	entries, _ := os.ReadDir(deps.ScansDir)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		scanDir := filepath.Join(deps.ScansDir, e.Name(), scanID)
		auditPath := filepath.Join(scanDir, "audit.json")
		if data, err := os.ReadFile(auditPath); err == nil { // #nosec G304 -- scansDir-bounded; scanID validated
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(data) // #nosec G705 -- application/json from disk, content-type set
			return
		}
		errPath := filepath.Join(scanDir, "error.json")
		if data, err := os.ReadFile(errPath); err == nil { // #nosec G304 -- scansDir-bounded; scanID validated
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusGone)
			_, _ = w.Write(data) // #nosec G705 -- application/json from disk, content-type set
			return
		}
	}
	WriteJSONError(w, http.StatusNotFound, fmt.Sprintf("scan %s not found", scanID))
}

// handleDeleteScan removes a scan directory from disk. Safe-delete: the dir
// must live under deps.ScansDir, the scan_id must pass safeScanID, and the
// resolved path must not escape the scans root. Returns 204 No Content on
// success, 404 if the scan does not exist, 400 if the scan_id is malformed,
// 500 if the underlying remove fails.
func handleDeleteScan(deps ScansDeps, scanID string, w http.ResponseWriter, _ *http.Request) {
	if !safeScanID(scanID) {
		WriteJSONError(w, http.StatusBadRequest, "invalid scan id")
		return
	}
	entries, _ := os.ReadDir(deps.ScansDir)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		candidate := filepath.Join(deps.ScansDir, e.Name(), scanID)
		info, err := os.Stat(candidate)
		if err != nil || !info.IsDir() {
			continue
		}
		// Belt-and-suspenders: ensure resolved abs path is still under
		// ScansDir before we recursively remove.
		absScansDir, _ := filepath.Abs(deps.ScansDir)
		absCandidate, _ := filepath.Abs(candidate)
		if !strings.HasPrefix(absCandidate+string(filepath.Separator), absScansDir+string(filepath.Separator)) {
			WriteJSONError(w, http.StatusInternalServerError, "refused to delete: path escapes scans root")
			return
		}
		if err := os.RemoveAll(absCandidate); err != nil {
			WriteJSONError(w, http.StatusInternalServerError, fmt.Sprintf("delete: %v", err))
			return
		}
		// Best-effort: if the target dir is now empty, remove it too so the
		// list endpoint doesn't show a phantom target.
		targetDir := filepath.Dir(absCandidate)
		if remaining, _ := os.ReadDir(targetDir); len(remaining) == 0 {
			_ = os.Remove(targetDir)
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	WriteJSONError(w, http.StatusNotFound, fmt.Sprintf("scan %s not found", scanID))
}

// Ensure imports stay used in this package (referenced by ScansDeps callers).
var (
	_ = claude.Client(nil)
	_ = scanner.Options{}
	_ = verdict.Verdict{}
)
