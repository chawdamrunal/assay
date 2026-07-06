//go:build integration

package scanner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chawdamrunal/assay/internal/claude"
)

// recordedToolUse mirrors claude.ToolUse with explicit snake_case JSON tags
// so recording files can use the natural API-shaped key names.
type recordedToolUse struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

// recordedResponse mirrors claude.Response for unmarshaling from JSON
// recordings (claude.Response has no JSON tags; tool_uses would not bind).
type recordedResponse struct {
	Text     string            `json:"text"`
	ToolUses []recordedToolUse `json:"tool_uses"`
	Stop     string            `json:"stop"`
}

// toClaudeResponse converts a recordedResponse to the runtime claude.Response.
func (r recordedResponse) toClaudeResponse() claude.Response {
	uses := make([]claude.ToolUse, len(r.ToolUses))
	for i, u := range r.ToolUses {
		uses[i] = claude.ToolUse{ID: u.ID, Name: u.Name, Input: u.Input}
	}
	return claude.Response{Text: r.Text, ToolUses: uses, Stop: r.Stop}
}

type recordedScan struct {
	Fixture            string             `json:"fixture"`
	ExpectedVerdict    string             `json:"expected_verdict"`
	ExpectedCategories []string           `json:"expected_categories"`
	Responses          []recordedResponse `json:"responses"`
}

// corpusFixtures maps recording file -> corpus subpath
var corpusFixtures = map[string]string{
	"rainbow-formatter.json":    "safe/rainbow-formatter",
	"aws-credential-exfil.json": "vulnerable/aws-credential-exfil",
}

// TestCorpusReplay drives the full scanner over each corpus fixture using
// pre-recorded FakeClient responses. Confirms the scanner produces the
// expected verdict and that vulnerable fixtures flag the expected category.
func TestCorpusReplay(t *testing.T) {
	repoRoot := findRepoRoot(t)

	for recordingFile, fixtureSubpath := range corpusFixtures {
		recordingFile := recordingFile
		fixtureSubpath := fixtureSubpath
		t.Run(fixtureSubpath, func(t *testing.T) {
			rec := loadRecording(t, filepath.Join(repoRoot, "testdata", "recorded", recordingFile))
			fixture := filepath.Join(repoRoot, "testdata", "corpus", fixtureSubpath)

			fc := claude.NewFakeClient()
			for _, r := range rec.Responses {
				fc.Enqueue(r.toClaudeResponse())
			}

			events := make(chan Event, 64)
			v, err := Scan(context.Background(), Options{
				Target:              fixture,
				Model:               "claude-sonnet-4-6",
				SubagentConcurrency: 1,
				Offline:             true,
			}, fc, events)
			close(events)

			require.NoError(t, err)
			require.NotNil(t, v)
			assert.Equal(t, rec.ExpectedVerdict, v.Verdict,
				"fixture %s: expected verdict %s, got %s", fixtureSubpath, rec.ExpectedVerdict, v.Verdict)

			gotCategories := map[string]bool{}
			for _, f := range v.Findings {
				gotCategories[f.Category] = true
			}
			for _, want := range rec.ExpectedCategories {
				assert.True(t, gotCategories[want],
					"fixture %s: expected category %s in findings, got %v", fixtureSubpath, want, gotCategories)
			}
		})
	}
}

func loadRecording(t *testing.T, path string) recordedScan {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err, "read recording %s", path)
	var r recordedScan
	require.NoError(t, json.Unmarshal(data, &r))
	return r
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	// Walk up until we find go.mod.
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repo root not found from " + wd)
		}
		dir = parent
	}
}
