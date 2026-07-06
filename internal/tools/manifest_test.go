package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseManifestPluginJSON(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "plugin.json"), []byte(`{
  "name": "rainbow-formatter",
  "version": "1.2.3",
  "description": "Formats code in rainbow colors",
  "source": "github://example/rainbow"
}`), 0o600))

	m := NewManifest(root)
	r, err := m.Parse(context.Background(), Invocation{Input: map[string]any{"path": "plugin.json"}})
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(r.Text), &parsed))
	assert.Equal(t, "rainbow-formatter", parsed["name"])
	assert.Equal(t, "1.2.3", parsed["version"])
	assert.Equal(t, "claude-code-plugin", parsed["kind"])
}

func TestParseManifestPackageJSON(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "package.json"), []byte(`{
  "name": "weather-mcp",
  "version": "0.5.0",
  "description": "MCP server for weather",
  "dependencies": {"axios": "^1.0.0", "express": "^4.18.0"}
}`), 0o600))

	m := NewManifest(root)
	r, err := m.Parse(context.Background(), Invocation{Input: map[string]any{"path": "package.json"}})
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(r.Text), &parsed))
	assert.Equal(t, "weather-mcp", parsed["name"])
	assert.Equal(t, "npm-package", parsed["kind"])
	deps, ok := parsed["dependencies"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "^1.0.0", deps["axios"])
}

func TestParseManifestMCPManifest(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "manifest.json"), []byte(`{
  "name": "github-mcp",
  "version": "0.1.0",
  "tools": [{"name": "list_repos", "description": "List GitHub repos"}],
  "scopes": ["read:repo"]
}`), 0o600))

	m := NewManifest(root)
	r, err := m.Parse(context.Background(), Invocation{Input: map[string]any{"path": "manifest.json"}})
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(r.Text), &parsed))
	assert.Equal(t, "github-mcp", parsed["name"])
	assert.Equal(t, "mcp-server", parsed["kind"])
	tools, ok := parsed["tools"].([]any)
	require.True(t, ok)
	assert.Len(t, tools, 1)
}

func TestParseManifestGoMod(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte(`module github.com/example/mcp

go 1.22

require (
	github.com/some/dep v1.2.3
	github.com/another/dep v0.5.0
)
`), 0o600))

	m := NewManifest(root)
	r, err := m.Parse(context.Background(), Invocation{Input: map[string]any{"path": "go.mod"}})
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(r.Text), &parsed))
	assert.Equal(t, "github.com/example/mcp", parsed["name"])
	assert.Equal(t, "go-module", parsed["kind"])
}

func TestParseManifestUnknownFile(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "random.txt"), []byte("just text"), 0o600))

	m := NewManifest(root)
	_, err := m.Parse(context.Background(), Invocation{Input: map[string]any{"path": "random.txt"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown manifest")
}

func TestParseManifestEscapeRejected(t *testing.T) {
	root := t.TempDir()
	m := NewManifest(root)
	_, err := m.Parse(context.Background(), Invocation{Input: map[string]any{"path": "../etc/passwd"}})
	require.Error(t, err)
}
