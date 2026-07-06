package inventory

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnumerateAllAggregatesEverything(t *testing.T) {
	root := t.TempDir()

	// Plugin
	pluginDir := filepath.Join(root, "plugins", "rainbow")
	require.NoError(t, os.MkdirAll(pluginDir, 0o750))
	require.NoError(t, os.WriteFile(
		filepath.Join(pluginDir, "plugin.json"),
		[]byte(`{"name":"rainbow","version":"1.0.0"}`), 0o600))

	// Settings (MCP + hooks + permissions)
	settings := `{
  "mcpServers": {"weather": {"command": "/usr/local/bin/weather"}},
  "hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "echo"}]}]},
  "permissions": {"allow": ["Bash(*)"]}
}`
	require.NoError(t, os.WriteFile(filepath.Join(root, "settings.json"), []byte(settings), 0o600))

	inv, err := EnumerateAll(EnumerateOptions{
		PluginsDir:   filepath.Join(root, "plugins"),
		SettingsFile: filepath.Join(root, "settings.json"),
	})
	require.NoError(t, err)
	require.False(t, inv.GeneratedAt.IsZero())

	kinds := map[Kind]int{}
	for _, it := range inv.Items {
		kinds[it.Kind]++
	}
	assert.Equal(t, 1, kinds[KindClaudeCodePlugin])
	assert.Equal(t, 1, kinds[KindMCPServer])
	assert.Equal(t, 1, kinds[KindHook])
	assert.Equal(t, 1, kinds[KindSettings])
}

func TestEnumerateAllHandlesAllMissing(t *testing.T) {
	root := t.TempDir()
	inv, err := EnumerateAll(EnumerateOptions{
		PluginsDir:   filepath.Join(root, "no-plugins"),
		SettingsFile: filepath.Join(root, "no-settings.json"),
	})
	require.NoError(t, err)
	assert.Empty(t, inv.Items)
}
