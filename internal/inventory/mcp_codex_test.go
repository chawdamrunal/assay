package inventory

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEnumerateMCPServersFromClaudeJSON guards the core "fetch system mcp
// connectors" fix: Claude Code persists user-scoped servers in ~/.claude.json
// (top-level mcpServers), and the same settings reader must parse that file
// while ignoring the large per-project `projects` section.
func TestEnumerateMCPServersFromClaudeJSON(t *testing.T) {
	tmp := t.TempDir()
	claudeJSON := `{
  "mcpServers": {
    "Amplitude": {"type": "http", "url": "https://mcp.amplitude.com/mcp"},
    "magic": {"type": "stdio", "command": "magic-server"}
  },
  "projects": {
    "/some/project": {"mcpServers": {"playwright": {"command": "pw"}}}
  }
}`
	path := filepath.Join(tmp, ".claude.json")
	require.NoError(t, os.WriteFile(path, []byte(claudeJSON), 0o600))

	items, err := EnumerateMCPServersFromSettings(path)
	require.NoError(t, err)
	require.Len(t, items, 2, "only top-level mcpServers; per-project entries are ignored")

	byName := map[string]Item{}
	for _, it := range items {
		assert.Equal(t, KindMCPServer, it.Kind)
		assert.Equal(t, path, it.Metadata["source"], "provenance points at the file it was read from")
		byName[it.Name] = it
	}
	require.Contains(t, byName, "Amplitude")
	require.Contains(t, byName, "magic")
	assert.NotContains(t, byName, "playwright", "per-project servers must not leak in")

	// Remote (http) connectors must capture type+url and not render blank.
	amp := byName["Amplitude"]
	assert.Equal(t, "http", amp.Metadata["type"])
	assert.Equal(t, "https://mcp.amplitude.com/mcp", amp.Metadata["url"])
	assert.Equal(t, "https://mcp.amplitude.com/mcp", amp.Metadata["commandLine"],
		"a command-less http server falls back to its URL for display")
	assert.Equal(t, "stdio", byName["magic"].Metadata["type"])
}

// TestEnumerateMCPServersFromCodex parses the [mcp_servers.*] TOML tables Codex
// uses, including a nested env table.
func TestEnumerateMCPServersFromCodex(t *testing.T) {
	tmp := t.TempDir()
	cfg := `
[mcp_servers.deepwiki]
command = "npx"
args = ["-y", "deepwiki-mcp"]

[mcp_servers.local]
command = "/usr/local/bin/local-mcp"

[mcp_servers.local.env]
TOKEN = "redacted"
`
	path := filepath.Join(tmp, "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(cfg), 0o600))

	items, err := EnumerateMCPServersFromCodex(path)
	require.NoError(t, err)
	require.Len(t, items, 2)

	byName := map[string]Item{}
	for _, it := range items {
		assert.Equal(t, KindMCPServer, it.Kind)
		byName[it.Name] = it
	}
	require.Contains(t, byName, "deepwiki")
	require.Contains(t, byName, "local")
	assert.Equal(t, "npx -y deepwiki-mcp", byName["deepwiki"].Metadata["commandLine"])
	assert.Equal(t, "TOKEN", byName["local"].Metadata["envKeys"])
	assert.Equal(t, path, byName["local"].Metadata["source"])
}

// TestEnumerateMCPServersFromCodexMissing — Codex may not be installed; a
// missing config is not an error, it just contributes nothing.
func TestEnumerateMCPServersFromCodexMissing(t *testing.T) {
	items, err := EnumerateMCPServersFromCodex(filepath.Join(t.TempDir(), "nope.toml"))
	require.NoError(t, err)
	assert.Empty(t, items)
}

// TestEnumerateAllMergesSystemMCPSources is the integration guard: EnumerateAll
// must discover MCP servers from settings.json, ~/.claude.json, AND Codex, and
// de-dupe a server declared in more than one file (claude.json provenance wins).
func TestEnumerateAllMergesSystemMCPSources(t *testing.T) {
	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude")
	require.NoError(t, os.MkdirAll(filepath.Join(claudeDir, "plugins"), 0o750))

	// settings.json: a duplicate of serverA (claude.json should win provenance).
	require.NoError(t, os.WriteFile(filepath.Join(claudeDir, "settings.json"),
		[]byte(`{"mcpServers": {"serverA": {"command": "from-settings"}}}`), 0o600))

	// ~/.claude.json: serverA (dup) + serverB.
	claudeJSON := filepath.Join(home, ".claude.json")
	require.NoError(t, os.WriteFile(claudeJSON,
		[]byte(`{"mcpServers": {"serverA": {"command": "from-claude-json"}, "serverB": {"command": "b"}}}`), 0o600))

	// ~/.codex/config.toml: serverC.
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".codex"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".codex", "config.toml"),
		[]byte("[mcp_servers.serverC]\ncommand = \"c\"\n"), 0o600))

	inv, err := EnumerateAll(OptionsForClaudeDir(claudeDir))
	require.NoError(t, err)

	mcps := map[string]Item{}
	for _, it := range inv.Items {
		if it.Kind == KindMCPServer {
			require.NotContains(t, mcps, it.Name, "each server appears exactly once after de-dupe")
			mcps[it.Name] = it
		}
	}
	require.Len(t, mcps, 3, "serverA (deduped) + serverB + serverC")
	assert.Contains(t, mcps, "serverA")
	assert.Contains(t, mcps, "serverB")
	assert.Contains(t, mcps, "serverC")
	assert.Equal(t, "from-claude-json", mcps["serverA"].Metadata["command"],
		"claude.json is read first, so it wins the de-dupe over settings.json")
	assert.Equal(t, claudeJSON, mcps["serverA"].Metadata["source"])
}
