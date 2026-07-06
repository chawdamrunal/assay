package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeriveTargetKind covers the deterministic kind classification that
// replaced the hardcoded "claude-code-plugin" in assembleVerdict. The verdict
// schema's target.kind enum depends on this being correct for non-plugin scans.
func TestDeriveTargetKind(t *testing.T) {
	mk := func(files map[string]string) string {
		dir := t.TempDir()
		for name, content := range files {
			require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
		}
		return dir
	}

	skillFM := "---\nname: x\ndescription: y\n---\n"

	assert.Equal(t, "claude-code-plugin", deriveTargetKind(mk(map[string]string{"plugin.json": "{}"})))
	assert.Equal(t, "claude-code-plugin", deriveTargetKind(mk(map[string]string{"claude-plugin.json": "{}"})))
	assert.Equal(t, "skill", deriveTargetKind(mk(map[string]string{"SKILL.md": skillFM})))
	assert.Equal(t, "mcp-server", deriveTargetKind(mk(map[string]string{".mcp.json": "{}"})))
	assert.Equal(t, "connector", deriveTargetKind(mk(map[string]string{"connector.json": `{"scopes":["read"]}`})))
	assert.Equal(t, "other", deriveTargetKind(mk(map[string]string{"random.txt": "hi"})))

	// A plugin bundle that also contains a skill + .mcp.json is still a plugin.
	assert.Equal(t, "claude-code-plugin",
		deriveTargetKind(mk(map[string]string{"plugin.json": "{}", "SKILL.md": skillFM, ".mcp.json": "{}"})))

	// A single SKILL.md file target (not a directory) classifies as skill.
	dir := mk(map[string]string{"SKILL.md": skillFM})
	assert.Equal(t, "skill", deriveTargetKind(filepath.Join(dir, "SKILL.md")))

	// A nonexistent path is "other", never a crash.
	assert.Equal(t, "other", deriveTargetKind(filepath.Join(t.TempDir(), "does-not-exist")))
}
