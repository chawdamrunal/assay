package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScansListEmpty(t *testing.T) {
	h := NewScansHandler(t.TempDir())

	req := httptest.NewRequest(http.MethodGet, "/api/scans", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Result().StatusCode)
	assert.JSONEq(t, `{"items":[]}`, w.Body.String())
}

func TestScansPostReturns501(t *testing.T) {
	h := NewScansHandler(t.TempDir())

	req := httptest.NewRequest(http.MethodPost, "/api/scans", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotImplemented, w.Result().StatusCode)
}

// TestScansPostRejectsPathOutsideAllowedRoots regression-guards the v0.5.1
// path-traversal fix. A request that names a target outside the configured
// allowed-roots whitelist must return 403, not silently proceed to scan
// /etc/passwd or similar.
func TestScansPostRejectsPathOutsideAllowedRoots(t *testing.T) {
	scansDir := t.TempDir()
	allowed := t.TempDir() // only this dir is in the whitelist

	called := false
	deps := ScansDeps{
		ScansDir: scansDir,
		Runner:   NewScanRunner(),
		StartScan: func(_ context.Context, _, _ string, _ bool, _ string, _ *ScanRunner) {
			called = true
		},
		AllowedRoots: []string{allowed},
	}
	h := NewScansHandler(deps)

	body := `{"target":"/etc/passwd"}`
	req := httptest.NewRequest(http.MethodPost, "/api/scans", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	require.Equal(t, http.StatusForbidden, w.Result().StatusCode)
	assert.Contains(t, w.Body.String(), "not allowed")
	assert.False(t, called, "StartScan must not run when the guard rejects")
}

// TestScansPostAcceptsAllowedPath confirms the guard lets legitimate paths
// through and the StartScan callback fires.
func TestScansPostAcceptsAllowedPath(t *testing.T) {
	scansDir := t.TempDir()
	allowed := t.TempDir()
	target := filepath.Join(allowed, "my-plugin")
	require.NoError(t, os.MkdirAll(target, 0o750))

	called := make(chan string, 1)
	deps := ScansDeps{
		ScansDir: scansDir,
		Runner:   NewScanRunner(),
		StartScan: func(_ context.Context, _, t string, _ bool, _ string, _ *ScanRunner) {
			called <- t
		},
		AllowedRoots: []string{allowed},
	}
	h := NewScansHandler(deps)

	body := `{"target":"` + target + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/scans", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	require.Equal(t, http.StatusAccepted, w.Result().StatusCode)
	select {
	case gotTarget := <-called:
		assert.Equal(t, filepath.Clean(target), gotTarget)
	default:
		// StartScan is invoked in a goroutine; give it a tick.
		require.Eventually(t, func() bool {
			select {
			case <-called:
				return true
			default:
				return false
			}
		}, time.Second, 10*time.Millisecond, "StartScan callback did not fire")
	}
}

func TestScansGetIDReturns404WhenMissing(t *testing.T) {
	h := NewScansHandler(t.TempDir())

	req := httptest.NewRequest(http.MethodGet, "/api/scans/unknown-id", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Result().StatusCode)
}

// TestScansListIncludesStatusAndCreatedAt asserts the list payload exposes the
// per-scan status (complete/failed/pending) and a created_at timestamp from
// the directory mtime, so the History page does not bucket UUID-IDed scans
// under "Unknown".
func TestScansListIncludesStatusAndCreatedAt(t *testing.T) {
	scansDir := t.TempDir()

	// Complete scan: has audit.json.
	completeDir := filepath.Join(scansDir, "alpha", "scan-complete")
	require.NoError(t, os.MkdirAll(completeDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(completeDir, "audit.json"), []byte(`{"scan_id":"scan-complete"}`), 0o600))

	// Failed scan: has error.json only.
	failedDir := filepath.Join(scansDir, "beta", "scan-failed")
	require.NoError(t, os.MkdirAll(failedDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(failedDir, "error.json"), []byte(`{"error":"boom"}`), 0o600))

	// Pending scan: empty dir.
	require.NoError(t, os.MkdirAll(filepath.Join(scansDir, "gamma", "scan-pending"), 0o750))

	h := NewScansHandler(ScansDeps{ScansDir: scansDir, Runner: NewScanRunner()})
	req := httptest.NewRequest(http.MethodGet, "/api/scans", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Result().StatusCode)

	var body struct {
		Items []map[string]any `json:"items"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))

	byID := map[string]map[string]any{}
	for _, it := range body.Items {
		byID[it["scan_id"].(string)] = it
	}
	require.Len(t, byID, 3)
	assert.Equal(t, "complete", byID["scan-complete"]["status"])
	assert.Equal(t, "failed", byID["scan-failed"]["status"])
	assert.Equal(t, "pending", byID["scan-pending"]["status"])
	for id, it := range byID {
		assert.NotEmpty(t, it["created_at"], "scan %s missing created_at", id)
	}
}

// TestScansGetIDReturnsErrorJSON asserts that a failed scan (with error.json
// but no audit.json) is served via GET /api/scans/:id as 410 Gone, so the
// SPA can render a real "scan failed" page instead of a bare 404.
func TestScansGetIDReturnsErrorJSON(t *testing.T) {
	scansDir := t.TempDir()
	failedDir := filepath.Join(scansDir, "tgt", "scan-failed")
	require.NoError(t, os.MkdirAll(failedDir, 0o750))
	errBody := `{"scan_id":"scan-failed","stage":"triage","error":"rate limited"}`
	require.NoError(t, os.WriteFile(filepath.Join(failedDir, "error.json"), []byte(errBody), 0o600))

	h := NewScansHandler(ScansDeps{ScansDir: scansDir, Runner: NewScanRunner()})
	req := httptest.NewRequest(http.MethodGet, "/api/scans/scan-failed", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusGone, w.Result().StatusCode)
	assert.JSONEq(t, errBody, w.Body.String())
}

// TestDeleteScanRemovesDirAndReturns204 covers the happy path: DELETE
// /api/scans/:id must wipe the scan directory and return 204.
func TestDeleteScanRemovesDirAndReturns204(t *testing.T) {
	scansDir := t.TempDir()
	scanDir := filepath.Join(scansDir, "target1", "scan-aaa")
	require.NoError(t, os.MkdirAll(scanDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(scanDir, "audit.json"), []byte(`{}`), 0o600))

	h := NewScansHandler(ScansDeps{ScansDir: scansDir, Runner: NewScanRunner()})
	req := httptest.NewRequest(http.MethodDelete, "/api/scans/scan-aaa", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Result().StatusCode)
	_, err := os.Stat(scanDir)
	assert.True(t, os.IsNotExist(err), "scan dir should be removed")

	// Empty target dir should also be cleaned up.
	_, err = os.Stat(filepath.Join(scansDir, "target1"))
	assert.True(t, os.IsNotExist(err), "empty target dir should be removed")
}

func TestDeleteScanReturns404WhenMissing(t *testing.T) {
	h := NewScansHandler(ScansDeps{ScansDir: t.TempDir(), Runner: NewScanRunner()})
	req := httptest.NewRequest(http.MethodDelete, "/api/scans/missing-id", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Result().StatusCode)
}

func TestDeleteScanRejectsBadID(t *testing.T) {
	h := NewScansHandler(ScansDeps{ScansDir: t.TempDir(), Runner: NewScanRunner()})
	req := httptest.NewRequest(http.MethodDelete, "/api/scans/..%2F..%2Fetc", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	// Either 400 (bad id) or 404 (decoded but not found) — both are safe.
	got := w.Result().StatusCode
	assert.True(t, got == http.StatusBadRequest || got == http.StatusNotFound,
		"got %d, want 400 or 404", got)
}

// TestDeleteScanLeavesOtherScansAlone confirms only the targeted scan_id is
// removed when multiple scans live under different target dirs.
func TestDeleteScanLeavesOtherScansAlone(t *testing.T) {
	scansDir := t.TempDir()
	keep := filepath.Join(scansDir, "alpha", "keep-id")
	gone := filepath.Join(scansDir, "beta", "gone-id")
	require.NoError(t, os.MkdirAll(keep, 0o750))
	require.NoError(t, os.MkdirAll(gone, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(keep, "audit.json"), []byte(`{}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(gone, "audit.json"), []byte(`{}`), 0o600))

	h := NewScansHandler(ScansDeps{ScansDir: scansDir, Runner: NewScanRunner()})
	req := httptest.NewRequest(http.MethodDelete, "/api/scans/gone-id", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusNoContent, w.Result().StatusCode)

	_, err := os.Stat(keep)
	assert.NoError(t, err, "untargeted scan should remain")
	_, err = os.Stat(gone)
	assert.True(t, os.IsNotExist(err))
}

func TestSafeScanID(t *testing.T) {
	cases := []struct {
		id   string
		want bool
	}{
		{"475ea351-29d8-4ec5-9b48-0571f752df9f", true},
		{"20260515T055441.436Z", true},
		{"20260515T055441.436Z-001", true},
		{"abc_def-123", true},
		{"", false},
		{"..", false},
		{"a/b", false},
		{"foo..bar", false},
		{"foo/../bar", false},
		{"id with spaces", false},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, safeScanID(c.id), "id=%q", c.id)
	}
}
