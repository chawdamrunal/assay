package tools

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecordFindingHappyPath(t *testing.T) {
	c := NewFindings()

	r, err := c.Record(context.Background(), Invocation{Input: map[string]any{
		"severity":         "critical",
		"category":         "exfiltration",
		"title":            "AWS credential exfiltration",
		"description":      "Reads ~/.aws/credentials and POSTs to evil.com",
		"exploit_scenario": "An attacker who installs this plugin gains AWS credentials.",
		"evidence": []any{
			map[string]any{
				"file":    "src/main.js",
				"line":    float64(42),
				"snippet": "fs.readFileSync(path.join(os.homedir(), '.aws/credentials'))",
			},
		},
	}})
	require.NoError(t, err)
	assert.Contains(t, r.Text, "recorded")

	all := c.All()
	require.Len(t, all, 1)
	assert.Equal(t, "critical", all[0].Severity)
	assert.Equal(t, "exfiltration", all[0].Category)
	require.Len(t, all[0].Evidence, 1)
	assert.Equal(t, "src/main.js", all[0].Evidence[0].File)
	assert.Equal(t, 42, all[0].Evidence[0].Line)
}

func TestRecordFindingValidatesSeverity(t *testing.T) {
	c := NewFindings()
	_, err := c.Record(context.Background(), Invocation{Input: map[string]any{
		"severity": "spicy",
		"category": "exfiltration",
		"title":    "x",
	}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "severity")
}

func TestRecordFindingValidatesCategory(t *testing.T) {
	c := NewFindings()
	_, err := c.Record(context.Background(), Invocation{Input: map[string]any{
		"severity": "high",
		"category": "made-up",
		"title":    "x",
	}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "category")
}

func TestRecordFindingRequiresTitle(t *testing.T) {
	c := NewFindings()
	_, err := c.Record(context.Background(), Invocation{Input: map[string]any{
		"severity": "high",
		"category": "injection",
	}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "title")
}

func TestRecordFindingInfoSeverityAllowsEmptyEvidence(t *testing.T) {
	// "No issues found" reports use severity=info with empty evidence.
	c := NewFindings()
	r, err := c.Record(context.Background(), Invocation{Input: map[string]any{
		"severity":    "info",
		"category":    "other",
		"title":       "No issues found for credential exfiltration",
		"description": "Investigated, found no credential reads or network egress.",
	}})
	require.NoError(t, err)
	assert.Contains(t, r.Text, "recorded")
	assert.Len(t, c.All(), 1)
}

func TestRecordFindingNonInfoRequiresEvidence(t *testing.T) {
	c := NewFindings()
	_, err := c.Record(context.Background(), Invocation{Input: map[string]any{
		"severity":    "high",
		"category":    "exfiltration",
		"title":       "Maybe something",
		"description": "I think...",
	}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "evidence")
}

func TestFindingsConcurrent(t *testing.T) {
	c := NewFindings()
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			_, _ = c.Record(context.Background(), Invocation{Input: map[string]any{
				"severity":    "info",
				"category":    "other",
				"title":       "concurrent",
				"description": "x",
			}})
			done <- struct{}{}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
	assert.Len(t, c.All(), 10)
}
