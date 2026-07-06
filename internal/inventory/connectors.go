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

// connectorManifestNames are the conventional filenames a locally-bundled
// connector declaration uses.
var connectorManifestNames = map[string]bool{
	"connector.json":          true,
	".connector.json":         true,
	"connector-manifest.json": true,
}

// EnumerateLocalConnectors returns one Item per local connector manifest under
// connectorsDir. claude.ai connectors are hosted and configured remotely, so
// most are not locally visible; this surfaces only connectors that ship a local
// manifest declaring OAuth scopes / endpoints. A missing directory returns an
// empty slice and nil error, like every other optional source.
func EnumerateLocalConnectors(connectorsDir string) ([]Item, error) {
	if connectorsDir == "" {
		return nil, nil
	}
	info, err := os.Stat(connectorsDir)
	if err != nil || !info.IsDir() {
		return nil, nil //nolint:nilerr // a missing connectors dir is not an error
	}

	var items []Item
	walkErr := filepath.WalkDir(connectorsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != connectorsDir && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !connectorManifestNames[d.Name()] {
			return nil
		}
		dir := filepath.Dir(path)
		name := filepath.Base(dir)
		if dir == connectorsDir { // a top-level connectorsDir/connector.json
			name = strings.TrimSuffix(d.Name(), ".json")
		}
		hash, _ := HashDir(dir)
		items = append(items, Item{
			Name:        name,
			Kind:        KindConnector,
			Source:      "local://" + dir,
			LocalPath:   dir,
			Permissions: readConnectorScopes(path),
			Hash:        hash,
		})
		return nil
	})
	return items, walkErr
}

// EnumerateClaudeAIConnectors reads claudeJSONPath (~/.claude.json) and returns
// one Item per claude.ai remote connector the user has connected. Claude Code
// records these under the top-level "claudeAiMcpEverConnected" array (e.g.
// "claude.ai Gmail", "claude.ai Notion"). They are hosted and configured
// remotely, so EnumerateLocalConnectors — which walks local manifest files —
// never sees them; surfacing them here gives the inventory visibility into the
// connector OAuth surface. The key is "ever connected", so an entry may be a
// connector the user has since removed; metadata records the remote provenance
// so the distinction from a locally-bundled connector stays legible. Missing
// file returns an empty slice, nil error, like every other optional source.
func EnumerateClaudeAIConnectors(claudeJSONPath string) ([]Item, error) {
	raw, err := os.ReadFile(claudeJSONPath) // #nosec G304 -- claudeJSONPath is a known config-file location
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", claudeJSONPath, err)
	}
	var c struct {
		Connectors []string `json:"claudeAiMcpEverConnected"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", claudeJSONPath, err)
	}
	names := append([]string(nil), c.Connectors...)
	sort.Strings(names)

	items := make([]Item, 0, len(names))
	seen := map[string]bool{}
	for _, name := range names {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		items = append(items, Item{
			Name:   name,
			Kind:   KindConnector,
			Source: "claudeai://remote",
			Metadata: map[string]string{
				"scope":    "remote",
				"provider": "claude.ai",
				"source":   claudeJSONPath,
			},
		})
	}
	return items, nil
}

// readConnectorScopes returns the declared OAuth scopes from a connector
// manifest, best-effort (empty on any read/parse error).
func readConnectorScopes(path string) []string {
	data, err := os.ReadFile(path) // #nosec G304 -- walk-bounded
	if err != nil {
		return nil
	}
	var m struct {
		Scopes []string `json:"scopes"`
	}
	if json.Unmarshal(data, &m) != nil {
		return nil
	}
	return m.Scopes
}
