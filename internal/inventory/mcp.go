package inventory

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
)

// mcpSettingsFile is the partial shape of settings.json relevant to MCP servers.
// hooks.go and settings.go define their own narrow shapes for their own concerns.
type mcpSettingsFile struct {
	MCPServers map[string]mcpServerConfig `json:"mcpServers"`
}

type mcpServerConfig struct {
	// Type is the transport: "stdio" (local command) or "http"/"sse" (remote
	// endpoint). Empty in older configs that imply stdio via Command.
	Type    string            `json:"type"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
	// URL is the endpoint for http/sse servers (no local Command). Captured so
	// remote connectors don't appear as blank, command-less entries.
	URL string `json:"url"`
}

// EnumerateMCPServersFromSettings reads settingsPath and returns one Item
// per declared MCP server. Missing file returns empty slice, nil error.
func EnumerateMCPServersFromSettings(settingsPath string) ([]Item, error) {
	raw, err := os.ReadFile(settingsPath) // #nosec G304 -- settingsPath is a known config-file location
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", settingsPath, err)
	}
	var s mcpSettingsFile
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("parse %s: %w", settingsPath, err)
	}
	// settingsPath is recorded as each item's provenance. The same top-level
	// {"mcpServers": {...}} shape is used by both ~/.claude/settings.json and
	// the user-scoped ~/.claude.json, so this function reads either file.
	return buildMCPItems(s.MCPServers, settingsPath), nil
}

// buildMCPItems converts a map of MCP server declarations into inventory Items,
// sorted by name for deterministic output. source is the absolute path the
// declarations were read from; it is recorded as metadata["source"] so the UI
// and report can show which config file each server came from (a scan target
// configured in ~/.claude.json looks different from one in Codex's config.toml).
func buildMCPItems(servers map[string]mcpServerConfig, source string) []Item {
	names := make([]string, 0, len(servers))
	for k := range servers {
		names = append(names, k)
	}
	sort.Strings(names)

	items := make([]Item, 0, len(names))
	for _, name := range names {
		cfg := servers[name]
		// commandLine summarizes how the server is reached: the full command
		// for stdio servers, or the endpoint URL for http/sse servers (which
		// have no local Command and would otherwise render blank).
		commandLine := cfg.Command
		if len(cfg.Args) > 0 {
			commandLine = cfg.Command + " " + strings.Join(cfg.Args, " ")
		}
		if commandLine == "" {
			commandLine = cfg.URL
		}
		transport := cfg.Type
		if transport == "" && cfg.Command != "" {
			transport = "stdio" // legacy configs imply stdio via a bare command
		}
		items = append(items, Item{
			Name:   name,
			Kind:   KindMCPServer,
			Source: "local://" + source,
			Metadata: map[string]string{
				"command":     cfg.Command,
				"commandLine": commandLine,
				"type":        transport,
				"url":         cfg.URL,
				"envKeys":     strings.Join(envKeys(cfg.Env), ","),
				"source":      source,
			},
		})
	}
	return items
}

// projectsFile is the partial shape of ~/.claude.json needed to reach the
// per-project MCP server declarations. Claude Code stores project-scoped
// servers under projects[<absolute path>].mcpServers — a location the
// top-level {"mcpServers":{}} reader (EnumerateMCPServersFromSettings) never
// sees, so those servers would otherwise go undetected entirely.
type projectsFile struct {
	Projects map[string]struct {
		MCPServers map[string]mcpServerConfig `json:"mcpServers"`
	} `json:"projects"`
}

// EnumerateProjectScopedMCPServers reads claudeJSONPath (~/.claude.json) and
// returns one Item per MCP server declared under a project entry. Each item is
// tagged metadata["scope"]="project" and metadata["project"]=<path> so the UI
// can show that a server is configured for a specific workspace rather than
// globally. Project paths are sorted first so provenance is deterministic when
// the same server name appears in more than one project. Missing file returns
// an empty slice, nil error, like every other optional source.
func EnumerateProjectScopedMCPServers(claudeJSONPath string) ([]Item, error) {
	raw, err := os.ReadFile(claudeJSONPath) // #nosec G304 -- claudeJSONPath is a known config-file location
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", claudeJSONPath, err)
	}
	var p projectsFile
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("parse %s: %w", claudeJSONPath, err)
	}
	paths := make([]string, 0, len(p.Projects))
	for path := range p.Projects {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	var items []Item
	for _, path := range paths {
		servers := p.Projects[path].MCPServers
		if len(servers) == 0 {
			continue
		}
		// buildMCPItems already initializes each item's Metadata map, so
		// annotating the project scope here is safe.
		for _, it := range buildMCPItems(servers, claudeJSONPath) {
			it.Metadata["scope"] = "project"
			it.Metadata["project"] = path
			items = append(items, it)
		}
	}
	return items, nil
}

func envKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
