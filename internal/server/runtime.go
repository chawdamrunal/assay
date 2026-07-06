// Package server wires production dependencies (paths, inventory loader)
// into the api package's Deps struct.
package server

import (
	"github.com/chawdamrunal/assay/internal/api"
	"github.com/chawdamrunal/assay/internal/inventory"
	"github.com/chawdamrunal/assay/internal/store"
)

// Runtime bundles the resolved configuration for `assay serve`.
type Runtime struct {
	Paths         *store.Paths
	ClaudeDir     string
	LoadInventory api.InventoryLoader
	ConfigPath    string
	ScansDir      string
}

// NewRuntime resolves the on-disk Claude dir and returns a Runtime.
// If claudeDir is empty, ResolveClaudeDir picks the OS-appropriate default
// (macOS/Linux: ~/.claude; Windows: %APPDATA%\Claude with ~/.claude as
// fallback). Users can always override via --claude-dir.
func NewRuntime(paths *store.Paths, claudeDir string) *Runtime {
	if claudeDir == "" {
		claudeDir = ResolveClaudeDir()
	}
	rt := &Runtime{
		Paths:      paths,
		ClaudeDir:  claudeDir,
		ConfigPath: paths.ConfigFile,
		ScansDir:   paths.ScansDir,
	}
	rt.LoadInventory = func() (inventory.Inventory, error) {
		return inventory.EnumerateAll(inventory.OptionsForClaudeDir(claudeDir))
	}
	return rt
}
