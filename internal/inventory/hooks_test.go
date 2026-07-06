package inventory

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnumerateHooksFromSettings(t *testing.T) {
	tmp := t.TempDir()
	settings := `{
  "hooks": {
    "PreToolUse": [
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "echo before-bash"}]}
    ],
    "PostToolUse": [
      {"matcher": ".*", "hooks": [{"type": "command", "command": "/usr/local/bin/audit-log"}]}
    ],
    "Stop": [
      {"matcher": "", "hooks": [{"type": "command", "command": "echo stopped"}]}
    ]
  }
}`
	path := filepath.Join(tmp, "settings.json")
	require.NoError(t, os.WriteFile(path, []byte(settings), 0o600))

	items, err := EnumerateHooksFromSettings(path)
	require.NoError(t, err)
	require.Len(t, items, 3)

	byEvent := map[string]Item{}
	for _, it := range items {
		byEvent[it.Metadata["event"]] = it
	}
	require.Contains(t, byEvent, "PreToolUse")
	require.Contains(t, byEvent, "PostToolUse")
	require.Contains(t, byEvent, "Stop")

	// Non-empty matcher → "<event>:<matcher>"
	assert.Equal(t, "PreToolUse:Bash", byEvent["PreToolUse"].Name)
	assert.Equal(t, "PostToolUse:.*", byEvent["PostToolUse"].Name)
	// Empty matcher → bare event name (no trailing colon)
	assert.Equal(t, "Stop", byEvent["Stop"].Name)

	for _, it := range items {
		assert.Equal(t, KindHook, it.Kind)
		assert.NotEmpty(t, it.Metadata["commands"])
	}
}

func TestEnumerateHooksMissingFile(t *testing.T) {
	items, err := EnumerateHooksFromSettings(filepath.Join(t.TempDir(), "nope.json"))
	require.NoError(t, err)
	assert.Empty(t, items)
}
