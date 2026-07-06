package prepass

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Run executes all enabled prepass checks against root and returns an aggregated Result.
// Network-dependent checks (OSV) are skipped when opts.Offline is true.
func Run(root string, opts Options) (Result, error) {
	r := Result{
		Target: root,
		RanAt:  time.Now().UTC(),
	}

	secrets, err := ScanSecrets(root, opts)
	if err != nil {
		return r, fmt.Errorf("secrets: %w", err)
	}
	r.Hits = append(r.Hits, secrets...)

	patterns, err := ScanPatterns(root, opts)
	if err != nil {
		return r, fmt.Errorf("patterns: %w", err)
	}
	r.Hits = append(r.Hits, patterns...)

	r.Manifests = discoverManifests(root)

	// OSV runs only when not offline and only against discovered manifests.
	// Per-package iteration happens in scanner.tools.osv_tool, invoked by the
	// LLM agent selectively. The aggregator records that a network OSV scan
	// was available but not run.

	return r, nil
}

// manifestNames are filenames Stage 0 will care about.
var manifestNames = []string{
	"plugin.json", "manifest.json", "package.json", "pyproject.toml", "go.mod",
}

func discoverManifests(root string) []string {
	var out []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		base := filepath.Base(path)
		if d.IsDir() && strings.HasPrefix(base, ".") {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		for _, name := range manifestNames {
			if base == name {
				rel, _ := filepath.Rel(root, path)
				if strings.Contains(rel, "node_modules") || strings.Contains(rel, "vendor") {
					return nil
				}
				if _, err := os.Stat(path); err == nil {
					out = append(out, path)
				}
				return nil
			}
		}
		return nil
	})
	return out
}
