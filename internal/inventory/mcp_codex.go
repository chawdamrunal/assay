package inventory

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/BurntSushi/toml"
)

// codexConfig is the partial shape of the Codex CLI config (~/.codex/config.toml)
// relevant to MCP servers. Codex declares each server as a [mcp_servers.<name>]
// table with the same command/args/env keys Claude Code uses in JSON.
type codexConfig struct {
	MCPServers map[string]codexMCPServer `toml:"mcp_servers"`
}

type codexMCPServer struct {
	Type    string            `toml:"type"`
	Command string            `toml:"command"`
	Args    []string          `toml:"args"`
	Env     map[string]string `toml:"env"`
	URL     string            `toml:"url"`
}

// EnumerateMCPServersFromCodex reads a Codex config.toml and returns one Item
// per declared MCP server. A missing file returns an empty slice and nil error
// — Codex may not be installed, which is not an inventory error. This mirrors
// EnumerateMCPServersFromSettings so both agent ecosystems' system MCP
// connectors surface in the same inventory.
func EnumerateMCPServersFromCodex(configPath string) ([]Item, error) {
	raw, err := os.ReadFile(configPath) // #nosec G304 -- configPath is a known config-file location
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", configPath, err)
	}
	var c codexConfig
	if err := toml.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", configPath, err)
	}
	// Re-key into the shared mcpServerConfig shape so buildMCPItems renders
	// Codex and Claude Code servers identically.
	servers := make(map[string]mcpServerConfig, len(c.MCPServers))
	for name, s := range c.MCPServers {
		servers[name] = mcpServerConfig{Type: s.Type, Command: s.Command, Args: s.Args, Env: s.Env, URL: s.URL}
	}
	return buildMCPItems(servers, configPath), nil
}
