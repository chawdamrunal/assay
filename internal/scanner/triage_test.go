package scanner

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chawdamrunal/assay/internal/claude"
)

func TestRunTriageParsesJSON(t *testing.T) {
	fc := claude.NewFakeClient()
	fc.Enqueue(claude.Response{
		Text: `{
  "declared_kind": "mcp-server",
  "declared_purpose": "Fetch weather data",
  "entry_points": ["src/main.js"],
  "permissions": ["network:read"],
  "files_to_inspect": ["src/main.js", "src/api.js"],
  "boilerplate": ["package-lock.json"],
  "notes": "No pre-pass concerns"
}`,
		Stop: "end_turn",
	})

	triage, err := RunTriage(context.Background(), TriageInput{
		Client: fc,
		Model:  "claude-sonnet-4-6",
	})
	require.NoError(t, err)
	assert.Equal(t, "mcp-server", triage.DeclaredKind)
	assert.Equal(t, "Fetch weather data", triage.DeclaredPurpose)
	assert.Equal(t, []string{"src/main.js"}, triage.EntryPoints)
}

func TestRunTriageRejectsInvalidJSON(t *testing.T) {
	fc := claude.NewFakeClient()
	fc.Enqueue(claude.Response{Text: "not json at all", Stop: "end_turn"})

	_, err := RunTriage(context.Background(), TriageInput{
		Client: fc,
		Model:  "claude-sonnet-4-6",
	})
	require.Error(t, err)
}

func TestRunTriageExtractsJSONFromMarkdownFence(t *testing.T) {
	fc := claude.NewFakeClient()
	fc.Enqueue(claude.Response{
		Text: "Here's my triage:\n\n```json\n{\"declared_kind\":\"hook\"}\n```\n\nDone.",
		Stop: "end_turn",
	})

	triage, err := RunTriage(context.Background(), TriageInput{
		Client: fc,
		Model:  "claude-sonnet-4-6",
	})
	require.NoError(t, err)
	assert.Equal(t, "hook", triage.DeclaredKind)
}
