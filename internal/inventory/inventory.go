package inventory

import (
	"fmt"
	"path/filepath"
	"time"
)

// EnumerateOptions controls which sources EnumerateAll reads.
//
// v0 reads a single settings file (either global ~/.claude/settings.json
// or a project-level .claude/settings.json). Merging both — and supporting
// project-level overrides — is a Plan 2 extension.
type EnumerateOptions struct {
	PluginsDir    string // e.g., ~/.claude/plugins
	SettingsFile  string // e.g., ~/.claude/settings.json
	SkillsDir     string // e.g., ~/.claude/skills; derived from PluginsDir's parent when empty
	ConnectorsDir string // e.g., ~/.claude/connectors; derived from PluginsDir's parent when empty
	// ClaudeJSONFile is the user-scoped Claude Code config (~/.claude.json).
	// Unlike settings.json, this is where Claude Code actually persists
	// user-level `mcpServers` (the system MCP connectors). Empty → not read,
	// which keeps unit tests hermetic (no accidental read of the real ~/.).
	ClaudeJSONFile string
	// CodexConfigFile is the Codex CLI config (~/.codex/config.toml), whose MCP
	// servers live under [mcp_servers.*]. Empty → not read.
	CodexConfigFile string
}

// OptionsForClaudeDir returns the standard enumeration options for a resolved
// Claude config dir, wiring every source Assay reads: plugins/skills/connectors
// under claudeDir, its settings.json, plus the user-scoped ~/.claude.json and
// Codex's ~/.codex/config.toml — both siblings of claudeDir (i.e. in $HOME).
// Centralizing this keeps the CLI, serve, and scan paths from drifting on which
// MCP-connector sources get discovered.
func OptionsForClaudeDir(claudeDir string) EnumerateOptions {
	home := filepath.Dir(claudeDir)
	return EnumerateOptions{
		PluginsDir:      filepath.Join(claudeDir, "plugins"),
		SettingsFile:    filepath.Join(claudeDir, "settings.json"),
		ClaudeJSONFile:  filepath.Join(home, ".claude.json"),
		CodexConfigFile: filepath.Join(home, ".codex", "config.toml"),
	}
}

// EnumerateAll runs every enumerator against opts and returns a combined Inventory.
// Sources that are missing or empty contribute zero items but never an error.
func EnumerateAll(opts EnumerateOptions) (Inventory, error) {
	inv := Inventory{GeneratedAt: time.Now().UTC()}

	// Prefer the installed_plugins.json manifest (current Claude Code layout).
	// Fall back to the directory walk for legacy installs without a manifest.
	installed, err := EnumerateInstalledPlugins(opts.PluginsDir)
	if err != nil {
		return inv, fmt.Errorf("installed plugins: %w", err)
	}
	if len(installed) > 0 {
		inv.Items = append(inv.Items, installed...)
	} else {
		plugins, err := EnumeratePlugins(opts.PluginsDir)
		if err != nil {
			return inv, fmt.Errorf("plugins: %w", err)
		}
		inv.Items = append(inv.Items, plugins...)
	}

	// MCP servers can be declared in several places. Claude Code persists
	// user-scoped servers in ~/.claude.json (NOT settings.json, whose
	// mcpServers is usually empty); Codex keeps them in ~/.codex/config.toml.
	// Read every configured source and de-dupe by name so a server present in
	// more than one file appears once. claude.json is consulted first so its
	// provenance wins on a name collision (it is the canonical system location).
	mcpSources := []struct {
		path string
		fn   func(string) ([]Item, error)
	}{
		{opts.ClaudeJSONFile, EnumerateMCPServersFromSettings},  // top-level user-scoped servers
		{opts.ClaudeJSONFile, EnumerateProjectScopedMCPServers}, // projects[*].mcpServers (per-workspace)
		{opts.SettingsFile, EnumerateMCPServersFromSettings},
		{opts.CodexConfigFile, EnumerateMCPServersFromCodex},
	}
	seenMCP := map[string]bool{}
	for _, src := range mcpSources {
		if src.path == "" {
			continue
		}
		mcps, err := src.fn(src.path)
		if err != nil {
			return inv, fmt.Errorf("mcp: %w", err)
		}
		for _, it := range mcps {
			if seenMCP[it.Name] {
				continue
			}
			seenMCP[it.Name] = true
			inv.Items = append(inv.Items, it)
		}
	}

	hooks, err := EnumerateHooksFromSettings(opts.SettingsFile)
	if err != nil {
		return inv, fmt.Errorf("hooks: %w", err)
	}
	inv.Items = append(inv.Items, hooks...)

	settings, err := EnumerateSettingsFromFile(opts.SettingsFile)
	if err != nil {
		return inv, fmt.Errorf("settings: %w", err)
	}
	inv.Items = append(inv.Items, settings...)

	// Standalone skills live under ~/.claude/skills (sibling of plugins/).
	// Derive that path from PluginsDir when the caller didn't set it explicitly,
	// so existing callers gain skill coverage without a signature change.
	skillsDir := opts.SkillsDir
	if skillsDir == "" && opts.PluginsDir != "" {
		skillsDir = filepath.Join(filepath.Dir(opts.PluginsDir), "skills")
	}
	skills, err := EnumerateStandaloneSkills(skillsDir)
	if err != nil {
		return inv, fmt.Errorf("skills: %w", err)
	}
	inv.Items = append(inv.Items, skills...)

	// Locally-bundled connector manifests live under ~/.claude/connectors.
	connectorsDir := opts.ConnectorsDir
	if connectorsDir == "" && opts.PluginsDir != "" {
		connectorsDir = filepath.Join(filepath.Dir(opts.PluginsDir), "connectors")
	}
	connectors, err := EnumerateLocalConnectors(connectorsDir)
	if err != nil {
		return inv, fmt.Errorf("connectors: %w", err)
	}
	inv.Items = append(inv.Items, connectors...)

	// claude.ai connectors are hosted remotely (no local manifest), so they are
	// recorded in ~/.claude.json rather than under connectors/. Surface them so
	// the connector OAuth surface is inventoried, de-duped by name against the
	// local connectors already added.
	remoteConnectors, err := EnumerateClaudeAIConnectors(opts.ClaudeJSONFile)
	if err != nil {
		return inv, fmt.Errorf("claude.ai connectors: %w", err)
	}
	seenConnector := make(map[string]bool, len(connectors))
	for _, c := range connectors {
		seenConnector[c.Name] = true
	}
	for _, c := range remoteConnectors {
		if seenConnector[c.Name] {
			continue
		}
		seenConnector[c.Name] = true
		inv.Items = append(inv.Items, c)
	}

	return inv, nil
}
