package server

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chawdamrunal/assay/internal/store"
)

func TestNewRuntimeUsesClaudeDirFlag(t *testing.T) {
	tmp := t.TempDir()
	paths := &store.Paths{
		ConfigDir:  tmp,
		ConfigFile: filepath.Join(tmp, "config.toml"),
		DataDir:    tmp,
		ScansDir:   filepath.Join(tmp, "scans"),
		CacheDir:   filepath.Join(tmp, "cache"),
	}

	rt := NewRuntime(paths, filepath.Join(tmp, "fake-claude"))

	assert.Equal(t, filepath.Join(tmp, "fake-claude"), rt.ClaudeDir)
	assert.Equal(t, paths.ConfigFile, rt.ConfigPath)
	assert.Equal(t, paths.ScansDir, rt.ScansDir)
	require.NotNil(t, rt.LoadInventory)

	// Calling LoadInventory against a non-existent Claude dir should not error;
	// inventory module returns empty for missing files.
	inv, err := rt.LoadInventory()
	require.NoError(t, err)
	assert.Empty(t, inv.Items)
}

func TestNewRuntimeDefaultsClaudeDir(t *testing.T) {
	tmp := t.TempDir()
	paths := &store.Paths{
		ConfigDir:  tmp,
		ConfigFile: filepath.Join(tmp, "config.toml"),
		DataDir:    tmp,
		ScansDir:   filepath.Join(tmp, "scans"),
		CacheDir:   filepath.Join(tmp, "cache"),
	}

	rt := NewRuntime(paths, "")
	// Empty claudeDir falls back to $HOME/.claude — just assert it's non-empty.
	assert.NotEmpty(t, rt.ClaudeDir)
}
