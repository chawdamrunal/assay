package scanner

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chawdamrunal/assay/internal/claude"
	"github.com/chawdamrunal/assay/internal/tools"
)

func TestRunInvestigationDispatchesOneAgentPerThreat(t *testing.T) {
	// Two threats → two sub-agents → two record_finding calls → two end_turn responses.
	// FakeClient is shared; responses are consumed in order. With concurrency=1, order is deterministic.
	fc := claude.NewFakeClient()

	// Sub-agent 1: records a critical finding, then ends.
	fc.Enqueue(claude.Response{
		ToolUses: []claude.ToolUse{{
			ID:   "rf1",
			Name: "record_finding",
			Input: map[string]any{
				"severity": "critical",
				"category": "exfiltration",
				"title":    "AWS creds read",
				"evidence": []any{
					map[string]any{"file": "src/main.js", "line": float64(42), "snippet": "fs.readFileSync('.aws/credentials')"},
				},
			},
		}},
		Stop: "tool_use",
	})
	fc.Enqueue(claude.Response{Text: "done", Stop: "end_turn"})

	// Sub-agent 2: reports no issues.
	fc.Enqueue(claude.Response{
		ToolUses: []claude.ToolUse{{
			ID:   "rf2",
			Name: "record_finding",
			Input: map[string]any{
				"severity": "info",
				"category": "other",
				"title":    "No issues found for capability mismatch",
			},
		}},
		Stop: "tool_use",
	})
	fc.Enqueue(claude.Response{Text: "done", Stop: "end_turn"})

	findings, openQs, err := RunInvestigation(context.Background(), InvestigationInput{
		Client: fc,
		Model:  "claude-sonnet-4-6",
		ThreatModel: ThreatModel{
			Threats: []Threat{
				{ID: "T1", Title: "AWS exfil", Description: "reads aws creds", ReviewerQuestions: []string{"q?"}},
				{ID: "T2", Title: "Capability mismatch", Description: "claims small, does more", ReviewerQuestions: []string{"q?"}},
			},
		},
		MaxConcurrency:      1,
		MaxTurnsPerSubagent: 10,
	})
	require.NoError(t, err)
	require.Len(t, findings, 2)
	assert.Empty(t, openQs)

	// Findings should be attributed to their threats.
	threatIDs := map[string]bool{}
	for _, f := range findings {
		threatIDs[f.ThreatID] = true
	}
	assert.True(t, threatIDs["T1"])
	assert.True(t, threatIDs["T2"])
}

func TestRunInvestigationBudgetExceededIsOpenQuestion(t *testing.T) {
	fc := claude.NewFakeClient()
	// First Complete returns enough usage to immediately exceed a tiny budget;
	// second call returns ErrBudgetExceeded from BudgetClient.
	fc.Enqueue(claude.Response{
		Text:  "done",
		Stop:  "end_turn",
		Usage: claude.Usage{InputTokens: 10_000_000, OutputTokens: 10_000_000},
	})

	b := claude.NewBudget(0.001)
	wrapped := claude.NewBudgetClient(fc, b)

	findings, openQs, err := RunInvestigation(context.Background(), InvestigationInput{
		Client: wrapped,
		Model:  "claude-sonnet-4-6",
		ThreatModel: ThreatModel{
			Threats: []Threat{
				{ID: "T1", Title: "x", Description: "y", ReviewerQuestions: []string{"q?"}},
				{ID: "T2", Title: "x2", Description: "y2", ReviewerQuestions: []string{"q?"}},
			},
		},
		MaxConcurrency:      1,
		MaxTurnsPerSubagent: 10,
	})
	require.NoError(t, err, "budget exceeded should not be a hard error")
	// First threat may or may not produce findings; the second should be recorded as open question.
	_ = findings
	require.NotEmpty(t, openQs, "expected at least one open question due to budget exceeded")
}

func TestRunInvestigationEmptyThreatModel(t *testing.T) {
	fc := claude.NewFakeClient()
	findings, openQs, err := RunInvestigation(context.Background(), InvestigationInput{
		Client:         fc,
		Model:          "claude-sonnet-4-6",
		ThreatModel:    ThreatModel{},
		MaxConcurrency: 1,
	})
	require.NoError(t, err)
	assert.Empty(t, findings)
	assert.Empty(t, openQs)

	// Use of tools package to ensure import path is valid even when unused.
	_ = tools.NewFindings()
}
