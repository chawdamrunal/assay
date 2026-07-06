package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chawdamrunal/assay/internal/claude"
)

func writeFile(dir, name, content string) error {
	return os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600)
}

func TestScanFlowEndToEndWithFakeClient(t *testing.T) {
	tmpDir := t.TempDir()
	// Create a tiny target
	require.NoError(t, writeFile(tmpDir, "plugin.json", `{"name":"x","version":"1.0.0"}`))
	require.NoError(t, writeFile(tmpDir, "main.js", "module.exports = function(){}"))

	fc := claude.NewFakeClient()
	// Stage 0: triage
	fc.Enqueue(claude.Response{Text: `{"declared_kind":"claude-code-plugin","declared_purpose":"test","entry_points":["main.js"],"permissions":[],"files_to_inspect":["main.js"],"boilerplate":[],"notes":""}`, Stop: "end_turn"})
	// Stage 1: claims
	fc.Enqueue(claude.Response{Text: `{"claims_paragraph":"No README","declared_capabilities":[],"declared_permissions":[],"declared_network":[],"declared_dependencies":[],"trust_signals":[]}`, Stop: "end_turn"})
	// Stage 2: threat model (one threat, so Stage 3 dispatches one sub-agent)
	fc.Enqueue(claude.Response{
		Text: "### T1: trivial\n**Class:** 7 Capability vs claim\n**Severity if exploited:** low\n**Description:** nothing scary\n**Reviewer questions:**\n- Anything weird?\n",
		Stop: "end_turn",
	})
	// Stage 3: sub-agent for T1 — record info, end
	fc.Enqueue(claude.Response{
		ToolUses: []claude.ToolUse{{
			ID: "rf", Name: "record_finding",
			Input: map[string]any{"severity": "info", "category": "other", "title": "No issues found"},
		}},
		Stop: "tool_use",
	})
	fc.Enqueue(claude.Response{Text: "done", Stop: "end_turn"})
	// Stage 4: exploitability — returns input unchanged
	fc.Enqueue(claude.Response{Text: `[{"severity":"info","category":"other","title":"No issues found"}]`, Stop: "end_turn"})
	// Stage 5: synthesis
	fc.Enqueue(claude.Response{Text: "# Audit\n\n**Verdict:** SAFE\n", Stop: "end_turn"})

	events := make(chan Event, 32)
	v, err := Scan(context.Background(), Options{
		Target:              tmpDir,
		Model:               "claude-sonnet-4-6",
		SubagentConcurrency: 1,
		Offline:             true,
	}, fc, events)
	close(events)

	require.NoError(t, err)
	require.NotNil(t, v)
	assert.Equal(t, "safe", v.Verdict)
	assert.NotEmpty(t, v.ScanID)
	_, err = uuid.Parse(v.ScanID)
	assert.NoError(t, err)

	// Drain events and check we saw each stage.
	stages := map[string]bool{}
	for e := range events {
		stages[e.Stage] = true
	}
	for _, s := range []string{"prepass", "triage", "claims", "threat_model", "investigation", "exploitability", "synthesis", "done"} {
		assert.True(t, stages[s], "expected event for stage %s", s)
	}
}
