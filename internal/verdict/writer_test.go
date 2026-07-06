package verdict

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteCreatesAllArtifacts(t *testing.T) {
	dir := t.TempDir()
	v := Verdict{
		SchemaVersion: "0.1",
		ScanID:        "00000000-0000-0000-0000-000000000001",
		Target:        Target{Kind: "claude-code-plugin", Name: "test", Version: "1.0.0"},
		ScannedAt:     time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC),
		Scanner:       Scanner{Name: "assay", Version: "0.1.0", Model: "claude-sonnet-4-6", PromptVersion: "v1"},
		Verdict:       "safe",
		Findings:      []Finding{},
	}

	err := Write(dir, v, "# Audit\n\nVerdict: SAFE", "investigation log content")
	require.NoError(t, err)

	// audit.json
	auditJSONPath := filepath.Join(dir, "audit.json")
	data, err := os.ReadFile(auditJSONPath) // #nosec G304 -- test tempdir

	require.NoError(t, err)
	var parsed Verdict
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, "safe", parsed.Verdict)
	assert.Equal(t, "test", parsed.Target.Name)

	// audit.md
	mdPath := filepath.Join(dir, "audit.md")
	md, err := os.ReadFile(mdPath) // #nosec G304 -- test tempdir

	require.NoError(t, err)
	assert.Contains(t, string(md), "# Audit")

	// investigation.log
	logPath := filepath.Join(dir, "investigation.log")
	logData, err := os.ReadFile(logPath) // #nosec G304 -- test tempdir

	require.NoError(t, err)
	assert.Contains(t, string(logData), "investigation log content")
}

func TestWriteRejectsInvalidDir(t *testing.T) {
	v := Verdict{SchemaVersion: "0.1", ScanID: "x", Verdict: "safe", Findings: []Finding{}}
	err := Write("/nonexistent/never/exists/path/abc123def456", v, "", "")
	require.Error(t, err)
}
