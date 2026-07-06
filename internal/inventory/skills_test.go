package inventory

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeSkill(t *testing.T, dir, name string) {
	t.Helper()
	sd := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(sd, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(sd, "SKILL.md"),
		[]byte("---\nname: "+name+"\ndescription: does "+name+"\n---\n# "+name+"\n"), 0o600))
}

func TestEnumerateStandaloneSkills(t *testing.T) {
	root := t.TempDir()
	skillsDir := filepath.Join(root, "skills")
	writeSkill(t, skillsDir, "formatter")
	writeSkill(t, skillsDir, "linter")

	items, err := EnumerateStandaloneSkills(skillsDir)
	require.NoError(t, err)
	require.Len(t, items, 2)
	byName := map[string]Item{}
	for _, it := range items {
		byName[it.Name] = it
	}
	f := byName["formatter"]
	assert.Equal(t, KindSkill, f.Kind)
	assert.Equal(t, filepath.Join(skillsDir, "formatter"), f.LocalPath)
	assert.NotEmpty(t, f.Hash, "skill item should carry a content hash for the scan cache")
}

func TestEnumerateStandaloneSkillsMissingDir(t *testing.T) {
	items, err := EnumerateStandaloneSkills(filepath.Join(t.TempDir(), "nope"))
	require.NoError(t, err)
	assert.Empty(t, items, "a missing skills dir is not an error and yields no items")
}

func TestEnumerateAllDerivesSkillsFromPluginsDir(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "skills"), "s1")

	inv, err := EnumerateAll(EnumerateOptions{
		PluginsDir:   filepath.Join(root, "plugins"),
		SettingsFile: filepath.Join(root, "settings.json"),
		// SkillsDir intentionally unset — must be derived from PluginsDir's parent.
	})
	require.NoError(t, err)
	var skillCount int
	for _, it := range inv.Items {
		if it.Kind == KindSkill {
			skillCount++
		}
	}
	assert.Equal(t, 1, skillCount, "EnumerateAll must derive skills/ from PluginsDir parent and include standalone skills")
}
