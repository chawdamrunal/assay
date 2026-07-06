package inventory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnumerateInstalledPluginsReadsManifestAndResolvesPaths(t *testing.T) {
	pluginsDir := t.TempDir()

	// Create two real install dirs that satisfy os.Stat.
	installA := filepath.Join(pluginsDir, "cache", "official", "plugin-a", "1.0.0")
	installB := filepath.Join(pluginsDir, "cache", "official", "plugin-b", "2.3.4")
	require.NoError(t, os.MkdirAll(installA, 0o755))
	require.NoError(t, os.MkdirAll(installB, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(installA, "README.md"), []byte("a"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(installB, "README.md"), []byte("b"), 0o644))

	manifest := installedPluginsFile{
		Version: 2,
		Plugins: map[string][]installedPluginScope{
			"plugin-a@official": {{
				Scope:       "user",
				InstallPath: installA,
				Version:     "1.0.0",
				InstalledAt: "2026-03-01T00:00:00Z",
			}},
			"plugin-b@official": {{
				Scope:       "user",
				InstallPath: installB,
				Version:     "2.3.4",
				InstalledAt: "2026-04-01T00:00:00Z",
			}},
			// Stale entry — install dir doesn't exist; must be silently skipped.
			"ghost@official": {{
				Scope:       "user",
				InstallPath: filepath.Join(pluginsDir, "nonexistent"),
				Version:     "0.0.0",
			}},
		},
	}
	data, _ := json.MarshalIndent(manifest, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(pluginsDir, "installed_plugins.json"), data, 0o600))

	items, err := EnumerateInstalledPlugins(pluginsDir)
	require.NoError(t, err)
	require.Len(t, items, 2, "ghost entry must be dropped")

	byName := map[string]Item{}
	for _, it := range items {
		byName[it.Name] = it
	}
	a, ok := byName["plugin-a"]
	require.True(t, ok)
	assert.Equal(t, KindClaudeCodePlugin, a.Kind)
	assert.Equal(t, "1.0.0", a.Version)
	assert.Equal(t, installA, a.LocalPath)
	assert.Equal(t, "official", a.Metadata["marketplace"])
	assert.Equal(t, "user", a.Metadata["scope"])
	assert.Contains(t, a.Source, "plugin-a@official")
	assert.NotEmpty(t, a.Hash, "hash should be populated for live dir")
}

func TestEnumerateInstalledPluginsHandlesMissingFile(t *testing.T) {
	items, err := EnumerateInstalledPlugins(t.TempDir())
	require.NoError(t, err)
	assert.Nil(t, items)
}

func TestSplitPluginKey(t *testing.T) {
	cases := []struct {
		key, name, market string
	}{
		{"frontend-design@claude-plugins-official", "frontend-design", "claude-plugins-official"},
		{"plain", "plain", ""},
		{"@nameless", "@nameless", ""}, // edge: empty name before @, LastIndex stays at 0 → falls back
	}
	for _, c := range cases {
		n, m := splitPluginKey(c.key)
		assert.Equal(t, c.name, n, "key=%s", c.key)
		assert.Equal(t, c.market, m, "key=%s", c.key)
	}
}
