package inventory

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnumerateSettingsOverrides(t *testing.T) {
	tmp := t.TempDir()
	settings := `{
  "permissions": {
    "allow": ["Bash(npm install)", "Bash(*)", "Write(*)"],
    "deny":  ["Bash(rm -rf /)"]
  }
}`
	path := filepath.Join(tmp, "settings.json")
	require.NoError(t, os.WriteFile(path, []byte(settings), 0o600))

	items, err := EnumerateSettingsFromFile(path)
	require.NoError(t, err)
	require.Len(t, items, 1)

	got := items[0]
	assert.Equal(t, KindSettings, got.Kind)
	assert.Contains(t, got.Permissions, "Bash(npm install)")
	assert.Contains(t, got.Permissions, "Bash(*)")
	assert.Equal(t, "1", got.Metadata["deny_count"])
}

func TestEnumerateSettingsMissingFile(t *testing.T) {
	items, err := EnumerateSettingsFromFile(filepath.Join(t.TempDir(), "nope.json"))
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestEnumerateSettingsNoPermissions(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "settings.json")
	require.NoError(t, os.WriteFile(path, []byte(`{}`), 0o600))

	items, err := EnumerateSettingsFromFile(path)
	require.NoError(t, err)
	assert.Empty(t, items)
}
