package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"

	"github.com/chawdamrunal/assay/internal/store"
)

// TestEndToEndFoundation exercises Task 1-17 together: config set/get + inventory + JSON.
func TestEndToEndFoundation(t *testing.T) {
	keyring.MockInit()

	// Fake Claude dir
	claudeDir := t.TempDir()
	pluginDir := filepath.Join(claudeDir, "plugins", "rainbow")
	require.NoError(t, os.MkdirAll(pluginDir, 0o750))
	require.NoError(t, os.WriteFile(
		filepath.Join(pluginDir, "plugin.json"),
		[]byte(`{"name":"rainbow","version":"1.0.0","source":"github://test/rainbow"}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "code.go"), []byte("package x"), 0o600))
	require.NoError(t, os.WriteFile(
		filepath.Join(claudeDir, "settings.json"),
		[]byte(`{"mcpServers":{"weather":{"command":"/usr/bin/weather","args":["--region","us-west"]}},"permissions":{"allow":["Bash(npm)"]}}`),
		0o600))

	// Fake Assay data dir + keyring
	dataRoot := t.TempDir()
	paths := &store.Paths{
		ConfigDir:  dataRoot,
		ConfigFile: filepath.Join(dataRoot, "config.toml"),
		DataDir:    dataRoot,
		ScansDir:   filepath.Join(dataRoot, "scans"),
		CacheDir:   filepath.Join(dataRoot, "cache"),
	}
	configOverridePaths = paths
	configKeyringService = "assay-test"

	run := func(args ...string) string {
		cmd := newRootCmd()
		cmd.SetArgs(args)
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		require.NoError(t, cmd.Execute())
		return out.String()
	}

	// config set / get round trip
	run("config", "set", "scan.budget_usd", "8.5")
	assert.Contains(t, run("config", "get", "scan.budget_usd"), "8.5")

	// api-key masking
	run("config", "set", "api-key", "sk-ant-secret-123")
	keyOut := run("config", "get", "api-key")
	assert.Contains(t, keyOut, "***")
	assert.NotContains(t, keyOut, "sk-ant-secret-123")

	// inventory table
	table := run("inventory", "--claude-dir", claudeDir)
	assert.Contains(t, table, "rainbow")
	assert.Contains(t, table, "weather")
	assert.Contains(t, table, "claude-code-plugin")
	assert.Contains(t, table, "mcp-server")
	assert.Contains(t, table, "settings")

	// inventory JSON validates and contains a non-empty hash for the plugin
	jsonOut := run("inventory", "--claude-dir", claudeDir, "--json")
	var inv struct {
		Items []struct {
			Name string `json:"name"`
			Kind string `json:"kind"`
			Hash string `json:"hash"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal([]byte(jsonOut), &inv))

	var foundPlugin bool
	for _, it := range inv.Items {
		if it.Kind == "claude-code-plugin" && it.Name == "rainbow" {
			foundPlugin = true
			assert.Contains(t, it.Hash, "sha256:")
		}
	}
	assert.True(t, foundPlugin, "expected to find rainbow plugin with hash in JSON output")
}
