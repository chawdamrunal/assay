package inventory

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// installedPluginsFile is the on-disk shape of
// ~/.claude/plugins/installed_plugins.json. Claude Code rewrote the plugin
// install layout: plugins no longer live as <pluginsDir>/<name>/plugin.json,
// they live in versioned cache subdirs and the active set is enumerated by
// this manifest. We read it directly so Assay sees the same plugins the user
// actually has installed.
type installedPluginsFile struct {
	Version int                               `json:"version"`
	Plugins map[string][]installedPluginScope `json:"plugins"`
}

type installedPluginScope struct {
	Scope        string `json:"scope"`
	InstallPath  string `json:"installPath"`
	Version      string `json:"version"`
	InstalledAt  string `json:"installedAt"`
	LastUpdated  string `json:"lastUpdated"`
	GitCommitSha string `json:"gitCommitSha"`
}

// EnumerateInstalledPlugins reads installed_plugins.json from the plugins
// root (typically ~/.claude/plugins/) and returns one Item per (name, scope)
// pair. The InstallPath becomes LocalPath so the scanner can read it.
// Missing manifest returns nil, nil — caller falls back to EnumeratePlugins.
//
// Plugin Name format from the manifest is "<name>@<marketplace>". We split
// these so the surfaced name is "<name>" and the marketplace lives in
// Metadata for the UI to display.
func EnumerateInstalledPlugins(pluginsDir string) ([]Item, error) {
	manifestPath := filepath.Join(pluginsDir, "installed_plugins.json")
	raw, err := os.ReadFile(manifestPath) // #nosec G304 -- pluginsDir-bound
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", manifestPath, err)
	}
	var f installedPluginsFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", manifestPath, err)
	}
	names := make([]string, 0, len(f.Plugins))
	for k := range f.Plugins {
		names = append(names, k)
	}
	sort.Strings(names)

	out := make([]Item, 0, len(names))
	for _, key := range names {
		name, marketplace := splitPluginKey(key)
		for _, sc := range f.Plugins[key] {
			if sc.InstallPath == "" {
				continue
			}
			// Skip ghost entries — install dir removed but manifest stale.
			if info, err := os.Stat(sc.InstallPath); err != nil || !info.IsDir() {
				continue
			}
			item := Item{
				Name:      name,
				Kind:      KindClaudeCodePlugin,
				Version:   sc.Version,
				LocalPath: sc.InstallPath,
				Source:    "claude-plugin://" + key,
				Metadata: map[string]string{
					"marketplace":  marketplace,
					"scope":        sc.Scope,
					"installed_at": sc.InstalledAt,
					"commit_sha":   sc.GitCommitSha,
				},
			}
			// Hash is expensive (filesystem walk); only run when path exists.
			if hash, err := HashDir(sc.InstallPath); err == nil {
				item.Hash = hash
			}
			out = append(out, item)
		}
	}
	return out, nil
}

// splitPluginKey turns "frontend-design@claude-plugins-official" into
// ("frontend-design", "claude-plugins-official"). Keys without an @ keep the
// raw string as name and an empty marketplace.
func splitPluginKey(key string) (name, marketplace string) {
	if i := strings.LastIndex(key, "@"); i > 0 {
		return key[:i], key[i+1:]
	}
	return key, ""
}
