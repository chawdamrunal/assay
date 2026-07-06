package scanner

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chawdamrunal/assay/internal/claude"
)

func TestRunClaimsParsesJSON(t *testing.T) {
	fc := claude.NewFakeClient()
	fc.Enqueue(claude.Response{
		Text: `{
  "claims_paragraph": "This MCP claims to fetch weather data from public APIs.",
  "declared_capabilities": ["get_forecast", "get_current"],
  "declared_permissions": [],
  "declared_network": ["api.weather.gov"],
  "declared_dependencies": ["axios"],
  "trust_signals": ["v0.5.0", "published by acme"]
}`,
		Stop: "end_turn",
	})

	c, err := RunClaims(context.Background(), ClaimsInput{
		Client: fc,
		Model:  "claude-sonnet-4-6",
		Triage: TriageMap{DeclaredKind: "mcp-server"},
	})
	require.NoError(t, err)
	assert.Contains(t, c.ClaimsParagraph, "weather data")
	assert.Equal(t, []string{"get_forecast", "get_current"}, c.DeclaredCapabilities)
	assert.Equal(t, []string{"api.weather.gov"}, c.DeclaredNetwork)
}

func TestRunClaimsHandlesEmptyReadme(t *testing.T) {
	fc := claude.NewFakeClient()
	fc.Enqueue(claude.Response{
		Text: `{
  "claims_paragraph": "No README or manifest claims available.",
  "declared_capabilities": [],
  "declared_permissions": [],
  "declared_network": [],
  "declared_dependencies": [],
  "trust_signals": []
}`,
		Stop: "end_turn",
	})

	c, err := RunClaims(context.Background(), ClaimsInput{
		Client: fc,
		Model:  "claude-sonnet-4-6",
		Triage: TriageMap{},
	})
	require.NoError(t, err)
	assert.Contains(t, c.ClaimsParagraph, "No README")
}
