package inventory

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnumerateMCPServersFromSettings(t *testing.T) {
	tmp := t.TempDir()
	settings := `{
  "mcpServers": {
    "weather": {
      "command": "/usr/local/bin/weather-mcp",
      "args": ["--region", "us-west"]
    },
    "github": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-github"],
      "env": {"GITHUB_TOKEN": "redacted"}
    }
  }
}`
	settingsPath := filepath.Join(tmp, "settings.json")
	require.NoError(t, os.WriteFile(settingsPath, []byte(settings), 0o600))

	items, err := EnumerateMCPServersFromSettings(settingsPath)
	require.NoError(t, err)
	require.Len(t, items, 2)

	names := []string{items[0].Name, items[1].Name}
	assert.Contains(t, names, "weather")
	assert.Contains(t, names, "github")

	for _, it := range items {
		assert.Equal(t, KindMCPServer, it.Kind)
		assert.NotEmpty(t, it.Metadata["command"])
	}
}

func TestEnumerateProjectScopedMCPServers(t *testing.T) {
	tmp := t.TempDir()
	claudeJSON := `{
  "mcpServers": {
    "globalServer": {"command": "/usr/bin/global"}
  },
  "projects": {
    "/home/u/proj-a": {
      "mcpServers": {
        "playwright": {"command": "npx", "args": ["-y", "@playwright/mcp"]}
      }
    },
    "/home/u/proj-b": {
      "mcpServers": {
        "playwright": {"command": "npx", "args": ["-y", "@playwright/mcp"]},
        "stitch": {"type": "http", "url": "https://stitch.example/mcp"}
      }
    },
    "/home/u/proj-empty": {}
  }
}`
	path := filepath.Join(tmp, ".claude.json")
	require.NoError(t, os.WriteFile(path, []byte(claudeJSON), 0o600))

	items, err := EnumerateProjectScopedMCPServers(path)
	require.NoError(t, err)
	// Two projects declare servers — proj-a (playwright) + proj-b
	// (playwright, stitch) = 3 items. The top-level "mcpServers" and the
	// empty project are ignored by this enumerator.
	require.Len(t, items, 3)

	for _, it := range items {
		assert.Equal(t, KindMCPServer, it.Kind)
		assert.Equal(t, "project", it.Metadata["scope"])
		assert.NotEmpty(t, it.Metadata["project"])
	}
	// Deterministic provenance: proj-a sorts before proj-b, so the first
	// playwright item is attributed to proj-a.
	assert.Equal(t, "playwright", items[0].Name)
	assert.Equal(t, "/home/u/proj-a", items[0].Metadata["project"])

	// An http server with no local command records its URL as commandLine.
	var stitch *Item
	for i := range items {
		if items[i].Name == "stitch" {
			stitch = &items[i]
		}
	}
	require.NotNil(t, stitch)
	assert.Equal(t, "https://stitch.example/mcp", stitch.Metadata["commandLine"])
}

func TestEnumerateProjectScopedMCPServersMissingFile(t *testing.T) {
	items, err := EnumerateProjectScopedMCPServers(filepath.Join(t.TempDir(), "nope.json"))
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestEnumerateMCPServersMissingFile(t *testing.T) {
	items, err := EnumerateMCPServersFromSettings(filepath.Join(t.TempDir(), "nope.json"))
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestEnumerateMCPServersNoMCPSection(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "settings.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"other":"data"}`), 0o600))

	items, err := EnumerateMCPServersFromSettings(path)
	require.NoError(t, err)
	assert.Empty(t, items)
}
