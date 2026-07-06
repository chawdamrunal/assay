package store

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigDefaults(t *testing.T) {
	c := DefaultConfig()
	// Default models are "auto" (empty) so Claude Code picks a model valid for
	// the user's subscription tier and we never break on a model retirement.
	assert.Equal(t, "", c.Models.Default)
	assert.Equal(t, "", c.Models.Investigation)
	assert.Equal(t, 3, c.Scan.SubagentConcurrency)
	assert.Equal(t, 5.0, c.Scan.BudgetUSD)
	assert.False(t, c.Telemetry.Enabled)
}

func TestConfigRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.toml")

	want := DefaultConfig()
	want.Scan.BudgetUSD = 12.5
	want.Scan.DeepScan = true
	want.Models.Investigation = "claude-opus-4-7"

	require.NoError(t, SaveConfig(path, want))

	got, err := LoadConfig(path)
	require.NoError(t, err)

	assert.Equal(t, want, got)
}

func TestLoadConfigMissingReturnsDefaults(t *testing.T) {
	got, err := LoadConfig(filepath.Join(t.TempDir(), "no-such-file.toml"))
	require.NoError(t, err)
	assert.Equal(t, DefaultConfig(), got)
}
