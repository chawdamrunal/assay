package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chawdamrunal/assay/internal/claude"
	"github.com/chawdamrunal/assay/internal/scanner"
	"github.com/chawdamrunal/assay/internal/store"
)

// TestScanCmdEndToEnd exercises `assay scan <path>` with a FakeClient injected
// via the scanClientFactory test seam.
func TestScanCmdEndToEnd(t *testing.T) {
	// Build a tiny target on disk.
	tgt := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tgt, "plugin.json"),
		[]byte(`{"name":"toy","version":"1.0.0"}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(tgt, "main.js"),
		[]byte("module.exports = function(){}\n"), 0o600))

	// Build a Paths with an isolated DataDir so we don't touch the real ~/.assay/.
	dataRoot := t.TempDir()
	paths := &store.Paths{
		ConfigDir:  dataRoot,
		ConfigFile: filepath.Join(dataRoot, "config.toml"),
		DataDir:    dataRoot,
		ScansDir:   filepath.Join(dataRoot, "scans"),
		CacheDir:   filepath.Join(dataRoot, "cache"),
	}
	require.NoError(t, paths.Ensure())
	configOverridePaths = paths

	// Inject a FakeClient that drives all 5 stages.
	fc := claude.NewFakeClient()
	// Stage 0 triage
	fc.Enqueue(claude.Response{Text: `{"declared_kind":"claude-code-plugin","declared_purpose":"toy","entry_points":["main.js"],"permissions":[],"files_to_inspect":["main.js"],"boilerplate":[],"notes":""}`, Stop: "end_turn"})
	// Stage 1 claims
	fc.Enqueue(claude.Response{Text: `{"claims_paragraph":"toy plugin","declared_capabilities":[],"declared_permissions":[],"declared_network":[],"declared_dependencies":[],"trust_signals":[]}`, Stop: "end_turn"})
	// Stage 2 threat model: one threat
	fc.Enqueue(claude.Response{Text: "### T1: nothing\n**Class:** 7 mismatch\n**Severity if exploited:** low\n**Description:** harmless\n**Reviewer questions:**\n- anything?\n", Stop: "end_turn"})
	// Stage 3 sub-agent: record info-severity (no issues)
	fc.Enqueue(claude.Response{
		ToolUses: []claude.ToolUse{{ID: "rf", Name: "record_finding",
			Input: map[string]any{"severity": "info", "category": "other", "title": "No issues found"}}},
		Stop: "tool_use",
	})
	fc.Enqueue(claude.Response{Text: "done", Stop: "end_turn"})
	// Stage 4 exploitability: keep input
	fc.Enqueue(claude.Response{Text: `[{"severity":"info","category":"other","title":"No issues found"}]`, Stop: "end_turn"})
	// Stage 5 synthesis
	fc.Enqueue(claude.Response{Text: "# Audit\n\n**Verdict:** SAFE\n", Stop: "end_turn"})

	scanClientFactory = func(_ context.Context) (claude.Client, error) { return fc, nil }
	defer func() { scanClientFactory = nil }()

	root := newRootCmd()
	root.SetArgs([]string{"scan", tgt, "--offline"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	require.NoError(t, root.Execute())

	output := out.String()
	// Task 4 replaced the old "=== Verdict: safe ===" banner with the shared
	// card, whose header is "### <badge> Assay: <verdict> — <name>".
	assert.Contains(t, output, "Assay:")
	assert.Contains(t, output, "safe")
	// Full scan must print the shared verdict card (CLI/PR parity with
	// --no-llm and the GitHub Action's PR comment).
	if !bytes.Contains(out.Bytes(), []byte("### ")) || !bytes.Contains(out.Bytes(), []byte("Assay:")) {
		t.Fatalf("full scan should print the verdict card, got:\n%s", output)
	}
	// The scan dir should now exist with audit.json + audit.md
	scanRoot := filepath.Join(paths.ScansDir)
	require.DirExists(t, scanRoot)
}

// TestScanCmdMissingTargetErrors confirms the command rejects missing args.
func TestScanCmdMissingTargetErrors(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"scan"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	err := root.Execute()
	require.Error(t, err)
}

// Ensure the imported scanner package is used (test seam reference).
var _ = scanner.Options{}

// TestCopyArtifactsToCopiesPresentFiles regression-guards the v0.5.1 fix for
// the silently-dropped --output flag. The helper must mkdir -p the dest and
// copy each present artifact byte-for-byte; missing artifacts are skipped
// without erroring (a scan that fails before audit.md is written is still
// valid input to --output as long as audit.json exists).
func TestCopyArtifactsToCopiesPresentFiles(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := filepath.Join(t.TempDir(), "out", "nested")
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "audit.json"), []byte(`{"v":1}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "audit.md"), []byte("# report"), 0o600))

	require.NoError(t, copyArtifactsTo(srcDir, dstDir))

	for _, name := range []string{"audit.json", "audit.md"} {
		data, err := os.ReadFile(filepath.Join(dstDir, name))
		require.NoError(t, err, name)
		assert.NotEmpty(t, data, name)
	}
}

func TestCopyArtifactsToSkipsMissingFiles(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "audit.json"), []byte(`{}`), 0o600))
	require.NoError(t, copyArtifactsTo(srcDir, dstDir))
	_, err := os.Stat(filepath.Join(dstDir, "audit.json"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(dstDir, "audit.md"))
	assert.True(t, os.IsNotExist(err))
}
