package inventory

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnumerateLocalConnectors(t *testing.T) {
	root := t.TempDir()
	cdir := filepath.Join(root, "connectors", "calendar")
	require.NoError(t, os.MkdirAll(cdir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(cdir, "connector.json"),
		[]byte(`{"name":"calendar","scopes":["calendar.read","mail.read"],"base_url":"https://api.example.com"}`), 0o600))

	items, err := EnumerateLocalConnectors(filepath.Join(root, "connectors"))
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "calendar", items[0].Name)
	assert.Equal(t, KindConnector, items[0].Kind)
	assert.Equal(t, []string{"calendar.read", "mail.read"}, items[0].Permissions)
	assert.Equal(t, cdir, items[0].LocalPath)
	assert.NotEmpty(t, items[0].Hash)
}

func TestEnumerateLocalConnectorsMissingDir(t *testing.T) {
	items, err := EnumerateLocalConnectors(filepath.Join(t.TempDir(), "nope"))
	require.NoError(t, err)
	assert.Empty(t, items, "a missing connectors dir is not an error and yields no items")
}

func TestEnumerateClaudeAIConnectors(t *testing.T) {
	tmp := t.TempDir()
	claudeJSON := `{
  "claudeAiMcpEverConnected": ["claude.ai Notion", "claude.ai Gmail", "claude.ai Notion", ""]
}`
	path := filepath.Join(tmp, ".claude.json")
	require.NoError(t, os.WriteFile(path, []byte(claudeJSON), 0o600))

	items, err := EnumerateClaudeAIConnectors(path)
	require.NoError(t, err)
	// Duplicates and empty strings are dropped; the rest are sorted by name.
	require.Len(t, items, 2)
	assert.Equal(t, "claude.ai Gmail", items[0].Name)
	assert.Equal(t, "claude.ai Notion", items[1].Name)
	for _, it := range items {
		assert.Equal(t, KindConnector, it.Kind)
		assert.Equal(t, "claude.ai", it.Metadata["provider"])
		assert.Equal(t, "remote", it.Metadata["scope"])
		assert.Equal(t, "claudeai://remote", it.Source)
	}
}

func TestEnumerateClaudeAIConnectorsMissingFile(t *testing.T) {
	items, err := EnumerateClaudeAIConnectors(filepath.Join(t.TempDir(), "nope.json"))
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestEnumerateAllDerivesConnectorsFromPluginsDir(t *testing.T) {
	root := t.TempDir()
	cdir := filepath.Join(root, "connectors", "c1")
	require.NoError(t, os.MkdirAll(cdir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(cdir, "connector.json"),
		[]byte(`{"name":"c1","scopes":["read"]}`), 0o600))

	inv, err := EnumerateAll(EnumerateOptions{
		PluginsDir:   filepath.Join(root, "plugins"),
		SettingsFile: filepath.Join(root, "settings.json"),
	})
	require.NoError(t, err)
	var n int
	for _, it := range inv.Items {
		if it.Kind == KindConnector {
			n++
		}
	}
	assert.Equal(t, 1, n, "EnumerateAll must derive connectors/ from PluginsDir parent and include local connectors")
}
