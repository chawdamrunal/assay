package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeAudit is a tiny helper that lays down a minimum-viable audit.json under
// scansDir/<target>/<scanID>/.
func writeAudit(t *testing.T, scansDir, target, scanID, body string) {
	t.Helper()
	dir := filepath.Join(scansDir, target, scanID)
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "audit.json"), []byte(body), 0o600))
}

func TestDiffEndpointReturnsAddedChangedResolvedStable(t *testing.T) {
	scansDir := t.TempDir()

	priorBody := `{
	  "schema_version": "0.1",
	  "scan_id": "p1",
	  "target": {"kind":"claude-code-plugin","name":"sample"},
	  "scanned_at": "2026-05-01T00:00:00Z",
	  "scanner": {"name":"assay","version":"0.4.0","model":"x","prompt_version":"mcp-v2"},
	  "verdict": "caution",
	  "findings": [
	    {"id":"P1","severity":"high","category":"exfil","title":"Reads creds","evidence":[{"file":"src/main.js","line":23,"snippet":"x"}]},
	    {"id":"P2","severity":"medium","category":"overscope","title":"Outbound post","evidence":[{"file":"src/main.js","line":10,"snippet":"y"}]}
	  ]
	}`
	currentBody := `{
	  "schema_version": "0.1",
	  "scan_id": "c1",
	  "target": {"kind":"claude-code-plugin","name":"sample"},
	  "scanned_at": "2026-05-08T00:00:00Z",
	  "scanner": {"name":"assay","version":"0.4.0","model":"x","prompt_version":"mcp-v2"},
	  "verdict": "unsafe",
	  "findings": [
	    {"id":"C1","severity":"high","category":"exfil","title":"Reads creds","evidence":[{"file":"src/main.js","line":23,"snippet":"x"}]},
	    {"id":"C2","severity":"critical","category":"exfil","title":"Reads ssh key","evidence":[{"file":"src/main.js","line":40,"snippet":"z"}]}
	  ]
	}`
	writeAudit(t, scansDir, "sample", "p1", priorBody)
	writeAudit(t, scansDir, "sample", "c1", currentBody)

	h := NewDiffHandler(scansDir)
	req := httptest.NewRequest(http.MethodGet, "/api/scans/diff?a=p1&b=c1", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Result().StatusCode, "body: %s", w.Body.String())

	var resp DiffResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotNil(t, resp.A)
	require.NotNil(t, resp.B)

	// Stable: P1↔C1 (same category, title, evidence file:line, same severity).
	assert.Len(t, resp.Stable, 1)
	if len(resp.Stable) == 1 {
		require.NotNil(t, resp.Stable[0].Diff)
		assert.Equal(t, "P1", resp.Stable[0].Diff.PriorID)
	}
	// Added: C2 has no prior match.
	assert.Len(t, resp.Added, 1)
	if len(resp.Added) == 1 {
		assert.Equal(t, "C2", resp.Added[0].ID)
		assert.Equal(t, "new", resp.Added[0].Diff.Status)
	}
	// Resolved: P2 has no current match.
	assert.Len(t, resp.Resolved, 1)
	if len(resp.Resolved) == 1 {
		assert.Equal(t, "P2", resp.Resolved[0].ID)
		assert.Equal(t, "resolved", resp.Resolved[0].Diff.Status)
	}
	// Changed: empty in this case.
	assert.Empty(t, resp.Changed)
}

func TestDiffEndpointRequiresBothParams(t *testing.T) {
	h := NewDiffHandler(t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/api/scans/diff?a=only", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
}

func TestDiffEndpoint404OnMissingScan(t *testing.T) {
	scansDir := t.TempDir()
	writeAudit(t, scansDir, "t", "exists", `{"schema_version":"0.1","scan_id":"exists","target":{"kind":"x","name":"t"},"scanned_at":"2026-01-01T00:00:00Z","scanner":{"name":"assay","version":"0","model":"x","prompt_version":"x"},"verdict":"safe","findings":[]}`)
	h := NewDiffHandler(scansDir)
	req := httptest.NewRequest(http.MethodGet, "/api/scans/diff?a=exists&b=missing", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Result().StatusCode)
}

func TestDiffEndpointRejectsBadID(t *testing.T) {
	h := NewDiffHandler(t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/api/scans/diff?a=ok&b=../etc", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
}
