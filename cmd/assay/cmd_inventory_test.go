package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupClaudeDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	pluginDir := filepath.Join(root, "plugins", "rainbow")
	require.NoError(t, os.MkdirAll(pluginDir, 0o750))
	require.NoError(t, os.WriteFile(
		filepath.Join(pluginDir, "plugin.json"),
		[]byte(`{"name":"rainbow","version":"1.0.0"}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "settings.json"), []byte(
		`{"mcpServers":{"weather":{"command":"/usr/bin/weather"}}}`), 0o600))
	return root
}

func TestInventoryTableOutput(t *testing.T) {
	root := setupClaudeDir(t)

	cmd := newRootCmd()
	cmd.SetArgs([]string{"inventory", "--claude-dir", root})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	require.NoError(t, cmd.Execute())

	output := out.String()
	assert.Contains(t, output, "rainbow")
	assert.Contains(t, output, "weather")
	assert.Contains(t, output, "claude-code-plugin")
	assert.Contains(t, output, "mcp-server")
}

func TestInventoryJSONOutput(t *testing.T) {
	root := setupClaudeDir(t)

	cmd := newRootCmd()
	cmd.SetArgs([]string{"inventory", "--claude-dir", root, "--json"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	require.NoError(t, cmd.Execute())

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &parsed))
	items, ok := parsed["items"].([]any)
	require.True(t, ok)
	assert.GreaterOrEqual(t, len(items), 2)
	assert.True(t, strings.HasPrefix(out.String(), "{"))
}

func TestInventoryEmptyState(t *testing.T) {
	tmp := t.TempDir() // empty dir - no plugins or settings

	cmd := newRootCmd()
	cmd.SetArgs([]string{"inventory", "--claude-dir", tmp})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	require.NoError(t, cmd.Execute())

	output := out.String()
	assert.Contains(t, output, "No plugins")
	assert.NotContains(t, output, "Total: 0")
}

func TestInventoryUsesHomeDirFallback(t *testing.T) {
	// Build a fake .claude dir
	tmp := t.TempDir()
	claudeDir := filepath.Join(tmp, ".claude")
	pluginDir := filepath.Join(claudeDir, "plugins", "rainbow")
	require.NoError(t, os.MkdirAll(pluginDir, 0o750))
	require.NoError(t, os.WriteFile(
		filepath.Join(pluginDir, "plugin.json"),
		[]byte(`{"name":"rainbow","version":"1.0.0"}`), 0o600))

	// Override homeDir so the default path points to our temp tree.
	original := homeDir
	homeDir = func() (string, error) { return tmp, nil }
	t.Cleanup(func() { homeDir = original })

	cmd := newRootCmd()
	cmd.SetArgs([]string{"inventory"}) // NO --claude-dir
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	require.NoError(t, cmd.Execute())

	assert.Contains(t, out.String(), "rainbow")
}
