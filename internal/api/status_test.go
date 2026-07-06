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

func TestStatusEndpointReturnsAllChecks(t *testing.T) {
	scansDir := t.TempDir()
	// Seed a complete scan and a failed scan so the filesystem probe
	// surfaces real counts.
	complete := filepath.Join(scansDir, "tgt", "scan-ok")
	failed := filepath.Join(scansDir, "tgt", "scan-fail")
	require.NoError(t, os.MkdirAll(complete, 0o750))
	require.NoError(t, os.MkdirAll(failed, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(complete, "audit.json"), []byte("{}"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(failed, "error.json"), []byte("{}"), 0o600))

	// Build a deps struct with a deliberately-missing claudeBin so the
	// probe goes through the not-on-PATH branch (warn level).
	h := NewStatusHandler(StatusDeps{
		ClaudeBin:  "definitely-not-a-real-binary-name-xyz",
		ScansDir:   scansDir,
		HookScript: filepath.Join(t.TempDir(), "nope.sh"),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Result().StatusCode)

	var resp StatusResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.GeneratedAt)
	require.Len(t, resp.Checks, 9) // 6 core probes + 3 per-provider API-key rows

	byKind := map[string]StatusCheck{}
	for _, c := range resp.Checks {
		byKind[c.Kind] = c
	}

	// Claude Code probe: warn because the bin name doesn't exist.
	assert.Equal(t, "warn", byKind["claude-code"].Level)
	assert.Contains(t, byKind["claude-code"].Detail, "PATH")

	// Filesystem probe: ok with counts.
	assert.Equal(t, "ok", byKind["filesystem"].Level)
	assert.Contains(t, byKind["filesystem"].Detail, "1 complete")
	assert.Contains(t, byKind["filesystem"].Detail, "1 failed")

	// Hook probe: warn because the script doesn't exist.
	assert.Equal(t, "warn", byKind["hook"].Level)
	assert.Contains(t, byKind["hook"].Detail, "not installed")

	// GitHub fetch probe: row exists with a populated detail. Level + detail
	// vary by host (git presence; whether a token resolves via env/gh CLI), so
	// we assert only that the row is present and non-empty.
	gh := byKind["github"]
	assert.NotEmpty(t, gh.Level)
	assert.NotEmpty(t, gh.Detail)

	// MCP probe runs the actual assay binary — in unit-test context that
	// will be the test binary, which doesn't speak MCP, so we expect an
	// error level. We just assert the row exists and has a populated detail.
	mcp := byKind["mcp"]
	assert.NotEmpty(t, mcp.Level)
	assert.NotEmpty(t, mcp.Detail)

	// Per-provider API-key rows: no KeychainService set in this test, so every
	// direct-API provider reports "warn" (no key configured).
	for _, kind := range []string{"anthropic-key", "gemini-key", "openai-key"} {
		assert.Equal(t, "warn", byKind[kind].Level, "expected %s row at warn", kind)
		assert.Contains(t, byKind[kind].Detail, "no key set")
	}
}

func TestStatusEndpointRejectsNonGET(t *testing.T) {
	h := NewStatusHandler(StatusDeps{})
	req := httptest.NewRequest(http.MethodPost, "/api/status", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Result().StatusCode)
}
