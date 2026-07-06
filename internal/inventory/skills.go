package inventory

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// EnumerateStandaloneSkills returns one Item per skill found under skillsDir.
// The standard layout is skillsDir/<name>/SKILL.md (plus an optional top-level
// skillsDir/SKILL.md). A missing directory returns an empty slice and nil error
// — skills are an optional source, like every other enumerator.
//
// Skills bundled *inside* a plugin are covered when that plugin is scanned;
// this enumerator surfaces user-installed standalone skills so `scan-all` and
// the inventory view reach them too.
func EnumerateStandaloneSkills(skillsDir string) ([]Item, error) {
	if skillsDir == "" {
		return nil, nil
	}
	info, err := os.Stat(skillsDir)
	if err != nil || !info.IsDir() {
		return nil, nil //nolint:nilerr // a missing skills dir is not an error
	}

	var items []Item
	walkErr := filepath.WalkDir(skillsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != skillsDir && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(d.Name(), "SKILL.md") {
			return nil
		}
		dir := filepath.Dir(path)
		name := filepath.Base(dir)
		if dir == skillsDir { // a top-level skillsDir/SKILL.md
			name = "SKILL"
		}
		hash, _ := HashDir(dir)
		items = append(items, Item{
			Name:      name,
			Kind:      KindSkill,
			Source:    "local://" + dir,
			LocalPath: dir,
			Hash:      hash,
		})
		return nil
	})
	return items, walkErr
}
