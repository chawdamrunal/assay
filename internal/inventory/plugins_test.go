package inventory

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnumeratePluginsDetectsValidPlugin(t *testing.T) {
	root := t.TempDir()
	pluginsDir := filepath.Join(root, "plugins")
	pluginDir := filepath.Join(pluginsDir, "rainbow-formatter")
	require.NoError(t, os.MkdirAll(pluginDir, 0o750))
	manifest := `{
  "name": "rainbow-formatter",
  "version": "1.2.3",
  "description": "Formats code in rainbow colors",
  "source": "github://example/rainbow-formatter"
}`
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(manifest), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "main.go"), []byte("package main"), 0o600))

	items, err := EnumeratePlugins(pluginsDir)
	require.NoError(t, err)
	require.Len(t, items, 1)

	got := items[0]
	assert.Equal(t, "rainbow-formatter", got.Name)
	assert.Equal(t, KindClaudeCodePlugin, got.Kind)
	assert.Equal(t, "1.2.3", got.Version)
	assert.Equal(t, "github://example/rainbow-formatter", got.Source)
	assert.Equal(t, pluginDir, got.LocalPath)
	assert.NotEmpty(t, got.Hash)
}

func TestEnumeratePluginsSkipsDirsWithoutManifest(t *testing.T) {
	root := t.TempDir()
	pluginsDir := filepath.Join(root, "plugins")
	bogus := filepath.Join(pluginsDir, "not-a-plugin")
	require.NoError(t, os.MkdirAll(bogus, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(bogus, "stuff.txt"), []byte("x"), 0o600))

	items, err := EnumeratePlugins(pluginsDir)
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestEnumeratePluginsHandlesMissingPluginsDir(t *testing.T) {
	items, err := EnumeratePlugins(filepath.Join(t.TempDir(), "no-such-dir"))
	require.NoError(t, err)
	assert.Empty(t, items)
}
