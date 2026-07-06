// Package store implements filesystem-backed persistence for Assay:
// config file, OS keychain for the API key, scan history, and cache.
package store

import (
	"fmt"
	"os"
	"path/filepath"
)

// Paths resolves and holds the on-disk locations Assay reads and writes.
type Paths struct {
	ConfigDir  string // e.g., ~/.config/assay
	ConfigFile string // e.g., ~/.config/assay/config.toml
	DataDir    string // e.g., ~/.assay
	ScansDir   string // e.g., ~/.assay/scans
	CacheDir   string // e.g., ~/.assay/cache
}

// NewPaths returns the standard XDG-aware Paths for the current user.
//
// Config lives under $XDG_CONFIG_HOME or ~/.config; data lives under ~/.assay
// (kept separate from XDG_DATA_HOME for discoverability).
func NewPaths() (*Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home dir: %w", err)
	}

	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	}

	configDir := filepath.Join(configHome, "assay")
	dataDir := filepath.Join(home, ".assay")

	return &Paths{
		ConfigDir:  configDir,
		ConfigFile: filepath.Join(configDir, "config.toml"),
		DataDir:    dataDir,
		ScansDir:   filepath.Join(dataDir, "scans"),
		CacheDir:   filepath.Join(dataDir, "cache"),
	}, nil
}

// Ensure creates every directory in Paths that does not already exist.
func (p *Paths) Ensure() error {
	for _, dir := range []string{p.ConfigDir, p.DataDir, p.ScansDir, p.CacheDir} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	return nil
}
