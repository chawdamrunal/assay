package scanner

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chawdamrunal/assay/internal/claude"
	"github.com/chawdamrunal/assay/internal/tools"
)

func TestRunSynthesisProducesAuditMarkdown(t *testing.T) {
	fc := claude.NewFakeClient()
	fc.Enqueue(claude.Response{
		Text: "# Assay Security Audit — test v1.0\n\n**Verdict:** UNSAFE\n\nDetails follow...",
		Stop: "end_turn",
	})

	v, err := RunSynthesis(context.Background(), SynthesisInput{
		Client: fc,
		Model:  "claude-sonnet-4-6",
		Target: "test",
		Findings: []tools.Finding{
			{Severity: "critical", Category: "exfiltration", Title: "AWS creds read"},
		},
	})
	require.NoError(t, err)
	assert.Contains(t, v.AuditMarkdown, "Assay Security Audit")
	assert.Equal(t, "unsafe", v.Verdict)
}

func TestComputeVerdict(t *testing.T) {
	cases := []struct {
		name     string
		findings []tools.Finding
		want     string
	}{
		{"empty", nil, "safe"},
		{"one info", []tools.Finding{{Severity: "info"}}, "safe"},
		{"three info", []tools.Finding{{Severity: "info"}, {Severity: "info"}, {Severity: "info"}}, "caution"},
		{"one low", []tools.Finding{{Severity: "low"}}, "safe"},
		{"three low", []tools.Finding{{Severity: "low"}, {Severity: "low"}, {Severity: "low"}}, "caution"},
		{"medium", []tools.Finding{{Severity: "medium"}}, "caution"},
		{"high", []tools.Finding{{Severity: "high"}}, "unsafe"},
		{"critical", []tools.Finding{{Severity: "critical"}}, "unsafe"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ComputeVerdict(tc.findings))
		})
	}
}

func TestSynthesisVerdictMatchesFindings(t *testing.T) {
	fc := claude.NewFakeClient()
	fc.Enqueue(claude.Response{Text: "# audit\n\n**Verdict:** SAFE\n", Stop: "end_turn"})

	v, err := RunSynthesis(context.Background(), SynthesisInput{
		Client:   fc,
		Model:    "m",
		Target:   "x",
		Findings: nil,
	})
	require.NoError(t, err)
	assert.Equal(t, "safe", v.Verdict)
	assert.True(t, strings.Contains(strings.ToLower(v.AuditMarkdown), "audit"))
}
