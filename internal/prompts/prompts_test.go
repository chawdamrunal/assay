package prompts

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadAllV1Prompts(t *testing.T) {
	names := []string{"triage", "claims", "threat_model", "investigator", "exploitability", "synthesis"}
	for _, n := range names {
		t.Run(n, func(t *testing.T) {
			text, err := Load("v1", n)
			require.NoError(t, err)
			assert.NotEmpty(t, text)
			assert.Greater(t, len(text), 200, "prompt should be substantial")
		})
	}
}

func TestLoadInvestigatorMentionsVerbatim(t *testing.T) {
	text, err := Load("v1", "investigator")
	require.NoError(t, err)
	assert.True(t, strings.Contains(strings.ToLower(text), "verbatim"),
		"investigator prompt must mention verbatim quotes (the citation hard rule)")
}

func TestLoadThreatModelMentionsBeforeCode(t *testing.T) {
	text, err := Load("v1", "threat_model")
	require.NoError(t, err)
	lower := strings.ToLower(text)
	assert.True(t,
		strings.Contains(lower, "before reading") || strings.Contains(lower, "before its source"),
		"threat_model prompt must emphasize threat modeling happens BEFORE reading source")
}

func TestLoadUnknownVersion(t *testing.T) {
	_, err := Load("v999", "triage")
	require.Error(t, err)
}

func TestLoadUnknownName(t *testing.T) {
	_, err := Load("v1", "nonexistent")
	require.Error(t, err)
}
