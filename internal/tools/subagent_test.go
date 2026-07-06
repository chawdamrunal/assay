package tools

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chawdamrunal/assay/internal/claude"
)

func TestDispatchSubagentRunsAgent(t *testing.T) {
	// The sub-agent calls record_finding once, then ends.
	fc := claude.NewFakeClient()
	fc.Enqueue(claude.Response{
		ToolUses: []claude.ToolUse{{
			ID:   "rf1",
			Name: "record_finding",
			Input: map[string]any{
				"severity": "info",
				"category": "other",
				"title":    "No issues found",
			},
		}},
		Stop: "tool_use",
	})
	fc.Enqueue(claude.Response{Text: "done", Stop: "end_turn"})

	parent := NewFindings()
	d := NewDispatcher(DispatcherConfig{
		Client:         fc,
		Model:          "claude-sonnet-4-6",
		System:         "you are an investigator",
		MaxConcurrency: 1,
		ParentFindings: parent,
	})

	r, err := d.Dispatch(context.Background(), Invocation{Input: map[string]any{
		"threat_id":          "T1",
		"threat_title":       "Credential exfiltration",
		"threat_description": "Reads ~/.aws/credentials",
		"reviewer_questions": []any{"Does it read .aws/?"},
	}})
	require.NoError(t, err)
	assert.Contains(t, r.Text, "T1")

	all := parent.All()
	require.Len(t, all, 1)
	assert.Equal(t, "T1", all[0].ThreatID)
	assert.Equal(t, "info", all[0].Severity)
}

func TestDispatchSubagentMissingThreatID(t *testing.T) {
	fc := claude.NewFakeClient()
	parent := NewFindings()
	d := NewDispatcher(DispatcherConfig{Client: fc, Model: "m", MaxConcurrency: 1, ParentFindings: parent})

	_, err := d.Dispatch(context.Background(), Invocation{Input: map[string]any{
		"threat_title": "x",
	}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "threat_id")
}
