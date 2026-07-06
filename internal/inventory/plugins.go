package inventory

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// pluginManifest captures the fields we read from plugin.json.
type pluginManifest struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Source      string `json:"source"`
}

// EnumeratePlugins lists Claude Code plugins under pluginsDir
// (typically ~/.claude/plugins/). A directory is considered a plugin
// if it contains a readable plugin.json. Missing pluginsDir returns
// an empty slice and a nil error.
func EnumeratePlugins(pluginsDir string) ([]Item, error) {
	entries, err := os.ReadDir(pluginsDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read plugins dir %s: %w", pluginsDir, err)
	}

	var items []Item
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(pluginsDir, e.Name())
		manifestPath := filepath.Join(dir, "plugin.json")
		raw, err := os.ReadFile(manifestPath) // #nosec G304 -- manifestPath built from pluginsDir/<entry>/plugin.json
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", manifestPath, err)
		}
		var m pluginManifest
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, fmt.Errorf("parse %s: %w", manifestPath, err)
		}
		name := m.Name
		if name == "" {
			name = e.Name()
		}
		hash, err := HashDir(dir)
		if err != nil {
			return nil, fmt.Errorf("hash %s: %w", dir, err)
		}
		items = append(items, Item{
			Name:      name,
			Kind:      KindClaudeCodePlugin,
			Version:   m.Version,
			Source:    m.Source,
			LocalPath: dir,
			Hash:      hash,
		})
	}
	return items, nil
}
