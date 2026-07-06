package scanner

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chawdamrunal/assay/internal/claude"
)

func TestRunThreatModelParsesMarkdownThreats(t *testing.T) {
	md := "### T1: AWS credential exfiltration\n" +
		"**Class:** 1 Credential / secret exfiltration\n" +
		"**Severity if exploited:** critical\n" +
		"**Description:** This plugin claims to format code, but if compromised could read ~/.aws/credentials.\n" +
		"**Reviewer questions:**\n" +
		"- Does the code read paths under ~/.aws/?\n" +
		"- Does it make outbound network calls?\n" +
		"- Is there any data flow from filesystem reads to network egress?\n" +
		"\n" +
		"### T2: Capability vs. claim mismatch\n" +
		"**Class:** 7 Capability vs. claim mismatch\n" +
		"**Severity if exploited:** high\n" +
		"**Description:** The README says it only formats; the code may do more.\n" +
		"**Reviewer questions:**\n" +
		"- Does the code only invoke formatting libraries?\n" +
		"- Are there any unexpected modules imported?\n"

	fc := claude.NewFakeClient()
	fc.Enqueue(claude.Response{Text: md, Stop: "end_turn"})

	tm, err := RunThreatModel(context.Background(), ThreatModelInput{
		Client: fc,
		Model:  "claude-sonnet-4-6",
	})
	require.NoError(t, err)
	require.Len(t, tm.Threats, 2)

	assert.Equal(t, "T1", tm.Threats[0].ID)
	assert.Equal(t, "AWS credential exfiltration", tm.Threats[0].Title)
	assert.Equal(t, "critical", tm.Threats[0].Severity)
	assert.Contains(t, tm.Threats[0].Class, "Credential")
	assert.Len(t, tm.Threats[0].ReviewerQuestions, 3)

	assert.Equal(t, "T2", tm.Threats[1].ID)
	assert.Equal(t, "high", tm.Threats[1].Severity)
	assert.Len(t, tm.Threats[1].ReviewerQuestions, 2)
}

func TestRunThreatModelEmptyOutput(t *testing.T) {
	fc := claude.NewFakeClient()
	fc.Enqueue(claude.Response{Text: "no threats parseable here", Stop: "end_turn"})

	tm, err := RunThreatModel(context.Background(), ThreatModelInput{
		Client: fc,
		Model:  "claude-sonnet-4-6",
	})
	require.NoError(t, err)
	assert.Empty(t, tm.Threats, "no threats parsed from unstructured output")
	assert.Equal(t, "no threats parseable here", tm.RawMarkdown)
}

func TestRunThreatModelTolerantOfWhitespace(t *testing.T) {
	// Variations: extra blank lines, lowercase severity, "Severity:" without "if exploited"
	md := "### T1: weird format\n" +
		"\n" +
		"**Class:** 5 Command execution\n" +
		"**Severity:** HIGH\n" +
		"\n" +
		"**Description:** something\n" +
		"\n" +
		"**Reviewer questions:**\n" +
		"- q1\n"

	fc := claude.NewFakeClient()
	fc.Enqueue(claude.Response{Text: md, Stop: "end_turn"})

	tm, err := RunThreatModel(context.Background(), ThreatModelInput{Client: fc, Model: "m"})
	require.NoError(t, err)
	require.Len(t, tm.Threats, 1)
	assert.Equal(t, "high", tm.Threats[0].Severity, "severity should be lowercased")
}
