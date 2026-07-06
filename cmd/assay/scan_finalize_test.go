package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestScanFinalized reports true iff audit.json is present — that file is the
// only proof assay_finalize_scan actually ran inside the MCP subprocess.
func TestScanFinalized(t *testing.T) {
	dir := t.TempDir()
	assert.False(t, scanFinalized(dir), "no audit.json → not finalized")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "audit.json"), []byte(`{"verdict":"safe"}`), 0o600))
	assert.True(t, scanFinalized(dir), "audit.json present → finalized")
}

// TestUnfinalizedScanFailure is the regression guard for the "scan vanished
// after refresh" bug: an MCP scan whose claude -p exited cleanly (spawnErr nil)
// but never wrote audit.json must leave an error.json behind, so the report
// route resolves it to a reachable "failed" state instead of a permanent 404.
func TestUnfinalizedScanFailure(t *testing.T) {
	t.Run("finalized scan is a no-op", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "audit.json"), []byte(`{"verdict":"safe"}`), 0o600))

		detail := unfinalizedScanFailure(dir, "scan-1", "/some/target", "")
		assert.Empty(t, detail, "a finalized scan needs no failure record")
		_, err := os.Stat(filepath.Join(dir, "error.json"))
		assert.True(t, os.IsNotExist(err), "must not write error.json over a finalized scan")
	})

	t.Run("clean exit without audit writes a reachable failure", func(t *testing.T) {
		dir := t.TempDir()
		// Mirror the orphaned-scan shape from the bug report: events + findings
		// recorded, meta present, but no audit.json and no error.json.
		require.NoError(t, os.WriteFile(filepath.Join(dir, "meta.json"), []byte(`{"status":"running"}`), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "findings.jsonl"), []byte("{}\n"), 0o600))

		detail := unfinalizedScanFailure(dir, "21f2c108", "/Users/me/.assay/sources/dvmcp", "max turns reached")
		require.NotEmpty(t, detail, "an unfinalized scan must produce a failure detail")
		assert.Contains(t, detail, "without producing a verdict")
		assert.Contains(t, detail, "max turns reached", "stderr/output tail should be threaded into the detail")

		data, err := os.ReadFile(filepath.Join(dir, "error.json"))
		require.NoError(t, err, "error.json must exist so /api/scans/:id resolves (410) instead of 404")
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		assert.Equal(t, "finalize", rec["stage"], "stage marks where the scan died")
		assert.Equal(t, "21f2c108", rec["scan_id"])
		assert.Equal(t, "/Users/me/.assay/sources/dvmcp", rec["target"], "target must round-trip so the FE Retry button can re-scan it")
		assert.NotEmpty(t, rec["error"])
		assert.NotEmpty(t, rec["failed_at"])
	})
}
