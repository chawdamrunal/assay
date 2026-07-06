// Package inventory enumerates Claude Code plugins, MCP servers, hooks,
// and settings overrides on the local machine.
package inventory

import "time"

// Kind classifies an inventoried item.
type Kind string

// Recognized Kind values for inventoried items. These mirror the
// target.kind enum in schemas/verdict-v0.1.json.
const (
	KindClaudeCodePlugin Kind = "claude-code-plugin"
	KindMCPServer        Kind = "mcp-server"
	KindConnector        Kind = "connector"
	KindSkill            Kind = "skill"
	KindHook             Kind = "hook"
	KindSettings         Kind = "settings"
)

// Item is a single inventoried artifact.
type Item struct {
	Name        string            `json:"name"`
	Kind        Kind              `json:"kind"`
	Version     string            `json:"version,omitempty"`
	Source      string            `json:"source,omitempty"` // e.g., github://owner/repo or local://path
	LocalPath   string            `json:"local_path,omitempty"`
	Permissions []string          `json:"permissions,omitempty"`
	Hash        string            `json:"hash,omitempty"` // sha256:<hex>
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// Inventory is the full enumeration result for one machine.
type Inventory struct {
	GeneratedAt time.Time `json:"generated_at"`
	Items       []Item    `json:"items"`
}
