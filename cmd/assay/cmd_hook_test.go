package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withFakeHome relocates HOME for the duration of a test so the hook
// install/uninstall round-trip can poke a sandbox ~/.claude without touching
// the real user's settings.
func withFakeHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	return tmp
}

func TestUpsertHookCreatesSettings(t *testing.T) {
	home := withFakeHome(t)
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	require.NoError(t, upsertHook(settingsPath, "/path/to/script.sh", 120))

	data, err := os.ReadFile(settingsPath)
	require.NoError(t, err)
	var root map[string]any
	require.NoError(t, json.Unmarshal(data, &root))
	hooks, ok := root["hooks"].(map[string]any)
	require.True(t, ok)
	ups, ok := hooks["UserPromptSubmit"].([]any)
	require.True(t, ok)
	require.Len(t, ups, 1)
	entry := ups[0].(map[string]any)
	require.Equal(t, ".*", entry["matcher"])
	inner := entry["hooks"].([]any)
	require.Len(t, inner, 1)
	h := inner[0].(map[string]any)
	assert.Equal(t, "command", h["type"])
	assert.Equal(t, "/path/to/script.sh", h["command"])
	assert.Equal(t, "managed-by:assay", h["managed_by"])
	assert.EqualValues(t, 120, h["timeout"])
}

// TestUpsertHookIsIdempotent ensures two installs don't accumulate entries.
func TestUpsertHookIsIdempotent(t *testing.T) {
	home := withFakeHome(t)
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	require.NoError(t, upsertHook(settingsPath, "/script.sh", 60))
	require.NoError(t, upsertHook(settingsPath, "/script.sh", 60))

	found, _, _ := readManagedHook(settingsPath)
	require.True(t, found)

	data, _ := os.ReadFile(settingsPath)
	var root map[string]any
	_ = json.Unmarshal(data, &root)
	hooks := root["hooks"].(map[string]any)
	ups := hooks["UserPromptSubmit"].([]any)
	assert.Len(t, ups, 1, "duplicate installs should not accumulate")
}

// TestUpsertHookPreservesUserEntries verifies we don't clobber hooks the
// user added by hand — only entries with the managed_by marker are touched.
func TestUpsertHookPreservesUserEntries(t *testing.T) {
	home := withFakeHome(t)
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	// Seed an unrelated user-managed UserPromptSubmit entry.
	require.NoError(t, os.MkdirAll(filepath.Dir(settingsPath), 0o750))
	seed := `{
	  "hooks": {
	    "UserPromptSubmit": [
	      {"matcher": "secret", "hooks": [{"type": "command", "command": "/usr/local/bin/whatever"}]}
	    ]
	  }
	}`
	require.NoError(t, os.WriteFile(settingsPath, []byte(seed), 0o600))

	require.NoError(t, upsertHook(settingsPath, "/assay.sh", 120))

	data, _ := os.ReadFile(settingsPath)
	var root map[string]any
	_ = json.Unmarshal(data, &root)
	hooks := root["hooks"].(map[string]any)
	ups := hooks["UserPromptSubmit"].([]any)
	assert.Len(t, ups, 2, "should append, not replace")

	// Now uninstall: the user entry must remain.
	removed, err := removeHook(settingsPath)
	require.NoError(t, err)
	require.True(t, removed)

	data, _ = os.ReadFile(settingsPath)
	_ = json.Unmarshal(data, &root)
	hooks = root["hooks"].(map[string]any)
	ups, _ = hooks["UserPromptSubmit"].([]any)
	require.Len(t, ups, 1, "user-managed entry must survive uninstall")
	left := ups[0].(map[string]any)
	assert.Equal(t, "secret", left["matcher"])
}

func TestRemoveHookNoOpWhenAbsent(t *testing.T) {
	home := withFakeHome(t)
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	removed, err := removeHook(settingsPath)
	require.NoError(t, err)
	assert.False(t, removed)
}

func TestResolvePluginRefFindsMarketplaceSource(t *testing.T) {
	home := withFakeHome(t)
	// Build a synthetic marketplaces tree.
	src := filepath.Join(home, ".claude", "plugins", "marketplaces", "demo-market", "plugins", "demo-plugin")
	require.NoError(t, os.MkdirAll(src, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(src, "plugin.json"), []byte(`{"name":"demo-plugin","version":"1"}`), 0o600))

	got, err := resolvePluginRef("demo-plugin@demo-market")
	require.NoError(t, err)
	assert.Equal(t, src, got)
}

func TestResolvePluginRefFallsBackToCache(t *testing.T) {
	home := withFakeHome(t)
	cacheDir := filepath.Join(home, ".claude", "plugins", "cache", "mkt", "name")
	// Two versions; lex-largest wins.
	require.NoError(t, os.MkdirAll(filepath.Join(cacheDir, "1.0.0"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(cacheDir, "2.5.0"), 0o750))

	got, err := resolvePluginRef("name@mkt")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(cacheDir, "2.5.0"), got)
}

func TestResolvePluginRefErrorOnUnknown(t *testing.T) {
	_ = withFakeHome(t)
	_, err := resolvePluginRef("nothing-here")
	require.Error(t, err)
}
