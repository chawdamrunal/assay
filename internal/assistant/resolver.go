package assistant

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chawdamrunal/assay/internal/inventory"
)

// Candidate is one scannable plugin or MCP server we can point a scan at.
type Candidate struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`       // "installed-plugin" | "marketplace-plugin" | "mcp-server"
	LocalPath   string `json:"local_path"` // absolute path the scanner will read
	Version     string `json:"version,omitempty"`
	Marketplace string `json:"marketplace,omitempty"`
	Description string `json:"description,omitempty"`
}

// Suggestion is a soft "did you mean?" hint returned alongside an empty
// Resolve result. The chat handler renders these as a small inline list so
// the user can correct a typo without re-typing the whole message.
type Suggestion struct {
	Name string
	Kind string
}

// Resolver answers "where does this plugin live on disk?" by consulting two
// sources, in priority order:
//
//  1. The user's installed inventory (already running plugins) — perfect match,
//     because it's the exact code the user runs today.
//
//  2. The marketplace cache at ~/.claude/plugins/marketplaces/<m>/plugins/<n>/
//     — code the user could install, useful for pre-install gating.
//
// GitHub fetch is deliberately omitted in v0.5; it lives in a future Resolver
// implementation that wraps this one.
type Resolver struct {
	LoadInventory   func() (inventory.Inventory, error)
	MarketplacesDir string // typically ~/.claude/plugins/marketplaces
}

// pluginManifest is a thin subset of marketplace plugin.json files. We only
// need name + description for candidate display; the rest stays opaque.
type pluginManifest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
}

// Resolve returns up to ~6 candidates that fuzzy-match `target`. Match logic:
//
//  1. Exact case-insensitive name match wins; ordered before substring hits.
//  2. Substring match on the plugin name covers "vercel" → "vercel-ai" etc.
//  3. Installed plugins always rank above marketplace-only candidates because
//     the user has them running today; that's the immediate threat model.
//
// Empty target returns nil — caller should branch on intent instead.
func (r *Resolver) Resolve(target string) ([]Candidate, error) {
	target = strings.ToLower(strings.TrimSpace(target))
	if target == "" {
		return nil, nil
	}

	var hits []Candidate

	// 1. Installed inventory.
	if r.LoadInventory != nil {
		inv, err := r.LoadInventory()
		if err == nil {
			seen := map[string]bool{}
			for _, item := range inv.Items {
				if item.LocalPath == "" {
					continue
				}
				if !nameMatches(item.Name, target) {
					continue
				}
				// De-dup by (name, path) — installed_plugins.json can list
				// the same plugin under multiple scopes.
				key := strings.ToLower(item.Name) + "|" + item.LocalPath
				if seen[key] {
					continue
				}
				seen[key] = true
				kind := "installed-plugin"
				if item.Kind == inventory.KindMCPServer {
					kind = "mcp-server"
				}
				hits = append(hits, Candidate{
					Name:        item.Name,
					Kind:        kind,
					LocalPath:   item.LocalPath,
					Version:     item.Version,
					Marketplace: item.Metadata["marketplace"],
				})
			}
		}
	}

	// 2. Marketplace cache (only when MarketplacesDir is set and exists).
	if r.MarketplacesDir != "" {
		seenPaths := pathSet(hits)
		mcands := walkMarketplaces(r.MarketplacesDir, target, seenPaths)
		hits = append(hits, mcands...)
	}

	// Rank: exact match first, then by Kind priority (installed > marketplace > mcp),
	// then alphabetically.
	sort.SliceStable(hits, func(i, j int) bool {
		ai := strings.ToLower(hits[i].Name) == target
		aj := strings.ToLower(hits[j].Name) == target
		if ai != aj {
			return ai
		}
		if hits[i].Kind != hits[j].Kind {
			return kindRank(hits[i].Kind) < kindRank(hits[j].Kind)
		}
		return hits[i].Name < hits[j].Name
	})

	// Cap to 6 — anything beyond that is noise.
	if len(hits) > 6 {
		hits = hits[:6]
	}
	return hits, nil
}

// Suggest returns up to `n` "closest names" from the user's inventory + the
// marketplace cache, ranked by Levenshtein distance to `target`. Used when
// Resolve returns no exact/substring match so the reply can say "did you
// mean X?" instead of a flat "I couldn't find it".
//
// Distance is bounded — names with a distance > max(3, len(target)/2) are
// dropped to avoid surfacing wildly unrelated noise. Empty target returns
// nil to keep the fallback paths simple.
func (r *Resolver) Suggest(target string, n int) []Suggestion {
	target = strings.ToLower(strings.TrimSpace(target))
	if target == "" || n <= 0 {
		return nil
	}
	maxDist := len(target) / 2
	if maxDist < 3 {
		maxDist = 3
	}

	type scored struct {
		s    Suggestion
		dist int
	}
	var pool []scored
	seen := map[string]bool{}
	push := func(name, kind string) {
		key := strings.ToLower(name) + "|" + kind
		if seen[key] {
			return
		}
		seen[key] = true
		d := levenshtein(strings.ToLower(name), target)
		if d <= maxDist {
			pool = append(pool, scored{Suggestion{Name: name, Kind: kind}, d})
		}
	}

	if r.LoadInventory != nil {
		if inv, err := r.LoadInventory(); err == nil {
			for _, item := range inv.Items {
				kind := "installed-plugin"
				if item.Kind == inventory.KindMCPServer {
					kind = "mcp-server"
				}
				push(item.Name, kind)
			}
		}
	}
	if r.MarketplacesDir != "" {
		walkMarketplacesAll(r.MarketplacesDir, push)
	}

	sort.SliceStable(pool, func(i, j int) bool { return pool[i].dist < pool[j].dist })
	out := make([]Suggestion, 0, n)
	for i := 0; i < len(pool) && i < n; i++ {
		out = append(out, pool[i].s)
	}
	return out
}

// walkMarketplacesAll iterates every plugin.json in the marketplace cache
// (no target filter), calling push(name, "marketplace-plugin") per entry.
// Used by Suggest. Quiet on read errors — best-effort.
func walkMarketplacesAll(rootDir string, push func(name, kind string)) {
	entries, err := os.ReadDir(rootDir)
	if err != nil {
		return
	}
	for _, mp := range entries {
		if !mp.IsDir() {
			continue
		}
		plugins, err := os.ReadDir(filepath.Join(rootDir, mp.Name(), "plugins"))
		if err != nil {
			continue
		}
		for _, p := range plugins {
			if !p.IsDir() {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(rootDir, mp.Name(), "plugins", p.Name(), "plugin.json")) // #nosec G304 -- bounded
			if err != nil {
				push(p.Name(), "marketplace-plugin")
				continue
			}
			var m pluginManifest
			if json.Unmarshal(raw, &m) == nil && m.Name != "" {
				push(m.Name, "marketplace-plugin")
			} else {
				push(p.Name(), "marketplace-plugin")
			}
		}
	}
}

// levenshtein returns the edit distance between two strings using the
// classic dynamic-programming algorithm. Used only for soft "did you mean"
// suggestions, so the O(n*m) cost is acceptable — names are short.
func levenshtein(a, b string) int {
	if a == b {
		return 0
	}
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			d := prev[j] + 1
			if curr[j-1]+1 < d {
				d = curr[j-1] + 1
			}
			if prev[j-1]+cost < d {
				d = prev[j-1] + cost
			}
			curr[j] = d
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

// nameMatches returns true when `name` looks like the user typed `target`.
// Case-insensitive equality wins; substring is a fallback to support partial
// names ("vercel" matches "vercel-ai-helper").
func nameMatches(name, target string) bool {
	n := strings.ToLower(name)
	if n == target {
		return true
	}
	return strings.Contains(n, target)
}

func kindRank(kind string) int {
	switch kind {
	case "installed-plugin":
		return 0
	case "marketplace-plugin":
		return 1
	case "mcp-server":
		return 2
	}
	return 3
}

func pathSet(c []Candidate) map[string]bool {
	out := make(map[string]bool, len(c))
	for _, x := range c {
		out[filepath.Clean(x.LocalPath)] = true
	}
	return out
}

// walkMarketplaces scans ~/.claude/plugins/marketplaces/<m>/plugins/<n>/ for
// plugin.json files whose `name` matches `target`. The directory layout we
// observe is verified from the user's real machine — Anthropic's plugin
// marketplace places plugin sources directly under `plugins/<name>/` with a
// plugin.json manifest at the root.
func walkMarketplaces(rootDir, target string, seen map[string]bool) []Candidate {
	entries, err := os.ReadDir(rootDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return nil
	}
	var out []Candidate
	for _, mp := range entries {
		if !mp.IsDir() {
			continue
		}
		pluginsRoot := filepath.Join(rootDir, mp.Name(), "plugins")
		plugins, err := os.ReadDir(pluginsRoot)
		if err != nil {
			continue
		}
		for _, p := range plugins {
			if !p.IsDir() {
				continue
			}
			localPath := filepath.Join(pluginsRoot, p.Name())
			if seen[filepath.Clean(localPath)] {
				continue
			}
			manifestPath := filepath.Join(localPath, "plugin.json")
			raw, err := os.ReadFile(manifestPath) // #nosec G304 -- bounded under marketplaces dir
			if err != nil {
				continue
			}
			var m pluginManifest
			if err := json.Unmarshal(raw, &m); err != nil {
				continue
			}
			name := m.Name
			if name == "" {
				name = p.Name()
			}
			if !nameMatches(name, target) {
				continue
			}
			out = append(out, Candidate{
				Name:        name,
				Kind:        "marketplace-plugin",
				LocalPath:   localPath,
				Version:     m.Version,
				Marketplace: mp.Name(),
				Description: m.Description,
			})
		}
	}
	return out
}
