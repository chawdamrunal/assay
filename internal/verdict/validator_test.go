package verdict

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidateExemptsDeterministicFloorSources guards the audited bypass fix:
// SCA/poison findings carry synthetic (manifest) evidence, so the file-content
// re-read must NOT apply to them — they're kept regardless — while an LLM
// finding with the same unverifiable evidence is still dropped.
func TestValidateExemptsDeterministicFloorSources(t *testing.T) {
	root := t.TempDir() // empty — no files exist to validate against

	findings := []Finding{
		{ID: "SCA-CVE-1", Severity: "high", Source: SourceSCA,
			Evidence: []Evidence{{File: "package-lock.json", Line: 1, Snippet: "left-pad@1.0.0"}}},
		{ID: "POISON-001-x", Severity: "critical", Source: SourcePoison,
			Evidence: []Evidence{{File: ".mcp.json", Line: 3, Snippet: "ignore previous instructions"}}},
		{ID: "F-1", Severity: "high", Source: SourceLLM,
			Evidence: []Evidence{{File: "nope.go", Line: 1, Snippet: "does not exist"}}},
	}

	kept, dropped := Validate(root, findings)

	keptIDs := map[string]bool{}
	for _, k := range kept {
		keptIDs[k.ID] = true
	}
	assert.True(t, keptIDs["SCA-CVE-1"], "SCA floor finding must be kept (synthetic evidence exempt)")
	assert.True(t, keptIDs["POISON-001-x"], "poison floor finding must be kept (synthetic evidence exempt)")
	assert.False(t, keptIDs["F-1"], "LLM finding with unverifiable evidence must still be dropped")
	require.Len(t, dropped, 1)
	assert.Equal(t, "F-1", dropped[0].ID)
}

func TestValidateKeepsMatchingFinding(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "src.js"),
		[]byte("const fs = require('fs');\nfs.readFileSync('.aws/credentials')\nconsole.log('done')\n"),
		0o600))

	findings := []Finding{
		{
			ID: "F1", Severity: "critical", Category: "exfiltration", Title: "AWS creds",
			Evidence: []Evidence{
				{File: "src.js", Line: 2, Snippet: "fs.readFileSync('.aws/credentials')"},
			},
		},
	}

	kept, dropped := Validate(dir, findings)
	require.Len(t, kept, 1)
	assert.Empty(t, dropped)
}

func TestValidateDropsConfabulatedFinding(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "src.js"),
		[]byte("function add(a, b) { return a + b; }\n"), 0o600))

	findings := []Finding{
		{
			ID: "F1", Severity: "critical", Category: "exfiltration", Title: "Fake finding",
			Evidence: []Evidence{
				{File: "src.js", Line: 1, Snippet: "exec(req.body.command)"}, // not in file
			},
		},
	}

	kept, dropped := Validate(dir, findings)
	assert.Empty(t, kept, "confabulated finding should be dropped")
	require.Len(t, dropped, 1)
	assert.Equal(t, "F1", dropped[0].ID)
	assert.Contains(t, dropped[0].Reason, "not found")
}

func TestValidateDropsForMissingFile(t *testing.T) {
	dir := t.TempDir()
	findings := []Finding{
		{
			ID: "F1", Severity: "critical", Category: "exfiltration", Title: "x",
			Evidence: []Evidence{
				{File: "nonexistent.js", Line: 1, Snippet: "anything"},
			},
		},
	}

	kept, dropped := Validate(dir, findings)
	assert.Empty(t, kept)
	require.Len(t, dropped, 1)
	assert.Contains(t, dropped[0].Reason, "file")
}

func TestValidateTolerantToWhitespace(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "x.go"),
		[]byte("package main\n\n  func main() { fmt.Println(\"hi\") }\n"), 0o600))

	findings := []Finding{
		{
			ID: "F1", Severity: "low", Category: "other", Title: "indented call",
			Evidence: []Evidence{
				// Snippet without leading spaces; file has leading spaces. Should still match.
				{File: "x.go", Line: 3, Snippet: "func main() { fmt.Println(\"hi\") }"},
			},
		},
	}
	kept, dropped := Validate(dir, findings)
	require.Len(t, kept, 1, "whitespace differences should not invalidate a citation; dropped=%v", dropped)
	assert.Empty(t, dropped)
}

func TestValidateChecksLineRange(t *testing.T) {
	// Snippet is in file but at a very different line — should still be found
	// in the ±3 line window? No — outside the window, it's a different finding.
	// We *do* allow ±3 line slack to forgive off-by-one errors.
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "y.go"),
		[]byte("a\nb\nc\nd\nMATCH HERE\ne\nf\ng\nh\n"), 0o600))

	findings := []Finding{
		{
			ID: "F1", Severity: "low", Category: "other", Title: "near-line",
			Evidence: []Evidence{
				{File: "y.go", Line: 4, Snippet: "MATCH HERE"}, // file has it at line 5; should be within ±3
			},
		},
	}
	kept, _ := Validate(dir, findings)
	require.Len(t, kept, 1, "off-by-one citation should be tolerated within ±3 line window")
}

func TestValidateNoEvidenceInfoSeverityKept(t *testing.T) {
	// info-severity "no issues found" findings have no evidence and are kept as-is.
	dir := t.TempDir()
	findings := []Finding{
		{ID: "F1", Severity: "info", Category: "other", Title: "No issues found"},
	}
	kept, dropped := Validate(dir, findings)
	require.Len(t, kept, 1)
	assert.Empty(t, dropped)
}

func TestValidatePartialEvidenceKept(t *testing.T) {
	// If one evidence entry validates and another doesn't, the finding is kept
	// with only the validated entries (preserves real evidence).
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "z.js"),
		[]byte("real_call()\n"), 0o600))

	findings := []Finding{
		{
			ID: "F1", Severity: "critical", Category: "exfiltration", Title: "two evidence",
			Evidence: []Evidence{
				{File: "z.js", Line: 1, Snippet: "real_call()"},
				{File: "z.js", Line: 1, Snippet: "fake_call()"},
			},
		},
	}
	kept, dropped := Validate(dir, findings)
	require.Len(t, kept, 1)
	assert.Len(t, kept[0].Evidence, 1, "only the validated evidence entry should remain")
	assert.Empty(t, dropped)
}
