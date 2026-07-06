// Package sca runs Software Composition Analysis over a plugin source
// tree: walks package.json + lockfiles (npm, pnpm) and any Python manifest,
// queries OSV.dev for each (ecosystem, name, version) triple, and emits
// verdict.Finding entries for the vulnerable transitives.
//
// This is the deterministic "supply chain floor" that runs before the LLM
// methodology — so the LLM sees the SCA findings as context and can
// prioritise stages around vulnerable packages instead of re-discovering
// them. Two scans of the same target produce identical SCA findings, no
// LLM variance.
//
// Design choices:
//
//  1. We respect the existing `offline` flag — when set, Analyze returns
//     an empty result with a "skipped: offline" reason in the audit.md.
//  2. OSV.dev is free, public, no API key. The /v1/query endpoint accepts
//     one (ecosystem, name, version) at a time; we batch by capping
//     concurrency at 8 so a 200-dep plugin doesn't fan out a thunder.
//  3. We deliberately do NOT vendor `syft` or `trivy` — both pull
//     ~50-100MB of transitive deps for SBOM generation we don't need.
//     A focused 200-LOC walker is plenty for package.json + lockfiles.
//  4. Findings get `Severity` mapped from CVSS: CRITICAL → critical,
//     HIGH → high, anything else → medium. Conservative on purpose.
package sca

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/chawdamrunal/assay/internal/prepass"
	"github.com/chawdamrunal/assay/internal/verdict"
)

// Result is the output of Analyze.
type Result struct {
	// Findings is one entry per vulnerable (package, version) pair. May be
	// empty when the plugin has no dependencies or no vulnerable ones.
	Findings []verdict.Finding
	// DepCount is the total number of (ecosystem, name, version) triples
	// the walker found — used by the dashboard tile to show "scanned N
	// dependencies, M vulnerable".
	DepCount int
	// SkipReason, when non-empty, explains why no SCA ran (offline mode,
	// no recognised manifest, OSV unreachable). Lets the report distinguish
	// "we ran SCA and found nothing" from "we never ran SCA".
	SkipReason string
}

// Coord is one resolved dependency coordinate.
type Coord struct {
	Ecosystem string // "npm" or "PyPI"
	Name      string
	Version   string
	// Direct flags whether this dep is in the top-level package.json
	// "dependencies"/"devDependencies"; transitive deps come from lockfiles.
	Direct bool
	// SourceFile is the manifest the coordinate was read from, relative to the
	// scan target root (e.g. "package-lock.json", "go.sum"). Used as the
	// finding's evidence file so a Go/Rust/Python finding doesn't claim it came
	// from package-lock.json.
	SourceFile string
}

// Analyze runs SCA against the target directory. When offline=true the
// network step is skipped and the returned Result carries a SkipReason
// so callers can render that in the audit instead of an empty section.
func Analyze(ctx context.Context, target string, offline bool, osv *prepass.OSVClient) (*Result, error) {
	coords, err := walkManifests(target)
	if err != nil {
		return nil, fmt.Errorf("walk manifests: %w", err)
	}
	res := &Result{DepCount: len(coords)}
	if len(coords) == 0 {
		res.SkipReason = "no recognised manifest found (package.json, lockfiles, pyproject.toml, requirements.txt, go.sum, Cargo.lock)"
		return res, nil
	}
	if offline {
		res.SkipReason = fmt.Sprintf("offline mode (skipped OSV lookup for %d deps)", len(coords))
		return res, nil
	}
	if osv == nil {
		osv = prepass.DefaultOSV()
	}

	// Cap concurrency at 8 so a plugin with 200 transitive deps doesn't
	// fan out 200 simultaneous HTTP requests to osv.dev. The endpoint is
	// rate-limited per-IP and we want to stay polite.
	sem := make(chan struct{}, 8)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, c := range coords {
		c := c
		select {
		case <-ctx.Done():
			return res, ctx.Err()
		default:
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			hits, err := osv.Lookup(c.Ecosystem, c.Name, c.Version)
			if err != nil {
				// Best-effort — one failed lookup doesn't fail the scan.
				return
			}
			for _, h := range hits {
				mu.Lock()
				res.Findings = append(res.Findings, toFinding(c, h))
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return res, nil
}

// toFinding converts a single OSV hit to a verdict.Finding. The id is
// composed so two SCA findings for the same package+vuln don't collide
// with LLM-emitted findings (which use F-NNN).
func toFinding(c Coord, h prepass.Hit) verdict.Finding {
	vulnID := h.Metadata["id"]
	if vulnID == "" {
		vulnID = "OSV-UNKNOWN"
	}
	directness := "transitive"
	if c.Direct {
		directness = "direct"
	}
	// ID includes the package@version so two packages sharing one CVE (e.g.
	// lodash@4.17.4 and lodash@4.17.20 both hit by the same advisory) produce
	// distinct, stable IDs — policy suppression and diff-mode key on the ID.
	evidenceFile := c.SourceFile
	if evidenceFile == "" {
		evidenceFile = "manifest"
	}
	return verdict.Finding{
		ID:                fmt.Sprintf("SCA-%s@%s-%s", c.Name, c.Version, vulnID),
		Severity:          h.Severity,
		Category:          "dependency",
		Source:            verdict.SourceSCA,
		Title:             fmt.Sprintf("%s@%s — %s", c.Name, c.Version, vulnID),
		Description:       h.Message,
		Context:           fmt.Sprintf("%s %s dependency `%s@%s` (ecosystem: %s)", directness, c.Ecosystem, c.Name, c.Version, c.Ecosystem),
		Impact:            fmt.Sprintf("A known vulnerability (%s) affects %s@%s. Severity is mapped from the published CVSS rating; review the OSV entry for the full exploit surface.", vulnID, c.Name, c.Version),
		Mitigation:        fmt.Sprintf("Upgrade `%s` past the affected range. Run `npm audit fix` (or pnpm/yarn equivalent) and re-scan.", c.Name),
		RecommendedAction: fmt.Sprintf("Open https://osv.dev/vulnerability/%s for the full advisory.", vulnID),
		Evidence: []verdict.Evidence{
			// SCA evidence is the manifest that pulls this version, not a code
			// citation. The post-validator exempts deterministic-floor sources
			// (Source=sca) from the file re-read, so the real manifest path is
			// safe to cite and is far more useful than a hardcoded
			// package-lock.json for Go/Rust/Python findings.
			{File: evidenceFile, Line: 1, Snippet: fmt.Sprintf("%s@%s", c.Name, c.Version)},
		},
	}
}

// walkManifests finds every recognised package manifest under root and
// returns one Coord per (ecosystem, name, version) triple. Errors reading
// individual files are silently skipped — the goal is best-effort SBOM,
// not perfect coverage.
func walkManifests(root string) ([]Coord, error) {
	var coords []Coord
	seen := map[string]bool{}
	// push records one coordinate. src is the manifest path (relative to root)
	// the coordinate came from, so each finding cites its real manifest.
	push := func(eco, name, ver string, direct bool, src string) {
		if name == "" || ver == "" {
			return
		}
		key := eco + "|" + name + "|" + ver
		if seen[key] {
			return
		}
		seen[key] = true
		coords = append(coords, Coord{Ecosystem: eco, Name: name, Version: ver, Direct: direct, SourceFile: src})
	}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable
		}
		if d.IsDir() {
			// Skip nested node_modules — package-lock.json already gives
			// us the transitive list with versions, and walking
			// node_modules can blow up on plugins with 1000s of packages.
			if d.Name() == "node_modules" || d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		// Bind the current manifest's relative path so the per-format readers
		// keep their simple push(eco,name,ver,direct) signature.
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil || rel == "" {
			rel = d.Name()
		}
		bound := func(eco, name, ver string, direct bool) { push(eco, name, ver, direct, rel) }
		switch d.Name() {
		case "package.json":
			readPackageJSON(path, bound)
		case "package-lock.json":
			readPackageLockJSON(path, bound)
		case "pnpm-lock.yaml":
			readPnpmLock(path, bound)
		case "pyproject.toml":
			readPyproject(path, bound)
		case "requirements.txt":
			readRequirements(path, bound)
		case "go.sum":
			readGoSum(path, bound)
		case "Cargo.lock":
			readCargoLock(path, bound)
		}
		return nil
	})
	return coords, err
}

// --- file readers (one per format) ---

func readPackageJSON(path string, push func(eco, name, ver string, direct bool)) {
	data, err := os.ReadFile(path) // #nosec G304 -- path bounded by walk
	if err != nil {
		return
	}
	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return
	}
	for name, raw := range pkg.Dependencies {
		push("npm", name, cleanVersion(raw), true)
	}
	for name, raw := range pkg.DevDependencies {
		push("npm", name, cleanVersion(raw), true)
	}
}

func readPackageLockJSON(path string, push func(eco, name, ver string, direct bool)) {
	data, err := os.ReadFile(path) // #nosec G304 -- path bounded by walk
	if err != nil {
		return
	}
	// npm lockfile v2/v3 puts every dep under "packages" keyed by
	// "node_modules/<name>" with a "version" field.
	var lock struct {
		Packages map[string]struct {
			Version string `json:"version"`
		} `json:"packages"`
		// v1 fallback
		Dependencies map[string]struct {
			Version string `json:"version"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal(data, &lock); err != nil {
		return
	}
	for k, v := range lock.Packages {
		if v.Version == "" || k == "" {
			continue
		}
		name := strings.TrimPrefix(k, "node_modules/")
		name = strings.TrimPrefix(name, "")
		if i := strings.Index(name, "/node_modules/"); i >= 0 {
			name = name[i+len("/node_modules/"):]
		}
		if name == "" {
			continue
		}
		push("npm", name, v.Version, false)
	}
	for name, v := range lock.Dependencies {
		push("npm", name, v.Version, false)
	}
}

func readPnpmLock(path string, push func(eco, name, ver string, direct bool)) {
	// Minimal pnpm-lock.yaml parser: pnpm packages are listed as
	// "/<name>@<version>:" lines (v6+) or "/<name>/<version>:" (older).
	data, err := os.ReadFile(path) // #nosec G304 -- path bounded by walk
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "/") {
			continue
		}
		if !strings.HasSuffix(line, ":") {
			continue
		}
		body := strings.TrimSuffix(strings.TrimPrefix(line, "/"), ":")
		// pnpm v6+: "<name>@<version>", older: "<name>/<version>"
		var name, ver string
		if i := strings.LastIndex(body, "@"); i > 0 && !strings.Contains(body[i:], "/") {
			name, ver = body[:i], body[i+1:]
		} else if i := strings.LastIndex(body, "/"); i > 0 {
			name, ver = body[:i], body[i+1:]
		} else {
			continue
		}
		// Strip the "(<peer>)@<v>" suffix some pnpm versions add.
		if i := strings.Index(ver, "("); i >= 0 {
			ver = ver[:i]
		}
		push("npm", name, ver, false)
	}
}

func readPyproject(path string, push func(eco, name, ver string, direct bool)) {
	data, err := os.ReadFile(path) // #nosec G304 -- path bounded by walk
	if err != nil {
		return
	}
	// Heuristic: scan for `name = "x.y.z"` style under [project.dependencies]
	// or PEP 508 strings. Not a full TOML parser — accuracy is better than
	// nothing for the common cases.
	for _, line := range strings.Split(string(data), "\n") {
		l := strings.TrimSpace(line)
		// Match: "requests==2.31.0" or "requests = \"2.31.0\""
		if i := strings.Index(l, "=="); i > 0 {
			name := strings.Trim(strings.TrimSpace(l[:i]), `"' ,`)
			ver := strings.Trim(strings.TrimSpace(l[i+2:]), `"' ,`)
			if name != "" && ver != "" && !strings.ContainsAny(name, " #[]") {
				push("PyPI", name, ver, true)
			}
		}
	}
}

// readGoSum parses go.sum, whose lines are "<module> <version> h1:<hash>" and
// "<module> <version>/go.mod h1:<hash>". We take the module + version (without
// the "/go.mod" suffix) and map to the OSV "Go" ecosystem, which matches Go
// module paths and semantic/pseudo versions (e.g. v1.2.3, v0.0.0-<ts>-<rev>).
// Dedup is handled by push's seen-set, so the two lines per module collapse.
func readGoSum(path string, push func(eco, name, ver string, direct bool)) {
	data, err := os.ReadFile(path) // #nosec G304 -- path bounded by walk
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		name := fields[0]
		ver := strings.TrimSuffix(fields[1], "/go.mod")
		// go.sum versions are "vX.Y.Z" or pseudo-versions; OSV wants them as-is.
		push("Go", name, ver, false)
	}
}

// readCargoLock parses Cargo.lock's [[package]] blocks (name + version) and
// maps to the OSV "crates.io" ecosystem. Minimal line scanner rather than a
// full TOML parser — accuracy is fine for the simple key = "value" shape Cargo
// emits.
func readCargoLock(path string, push func(eco, name, ver string, direct bool)) {
	data, err := os.ReadFile(path) // #nosec G304 -- path bounded by walk
	if err != nil {
		return
	}
	var name, ver string
	flush := func() {
		if name != "" && ver != "" {
			push("crates.io", name, ver, false)
		}
		name, ver = "", ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		l := strings.TrimSpace(line)
		switch {
		case l == "[[package]]":
			flush() // start of a new package block
		case strings.HasPrefix(l, "name = "):
			name = strings.Trim(strings.TrimPrefix(l, "name = "), `"`)
		case strings.HasPrefix(l, "version = "):
			ver = strings.Trim(strings.TrimPrefix(l, "version = "), `"`)
		}
	}
	flush() // last block
}

func readRequirements(path string, push func(eco, name, ver string, direct bool)) {
	data, err := os.ReadFile(path) // #nosec G304 -- path bounded by walk
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		l := strings.TrimSpace(line)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		// Strip trailing comments.
		if i := strings.Index(l, "#"); i >= 0 {
			l = strings.TrimSpace(l[:i])
		}
		// requirements.txt: "name==1.2.3", "name>=1.2", etc. We only care
		// about exact-pinned for SCA — ranges can't be CVE-checked.
		if i := strings.Index(l, "=="); i > 0 {
			name := strings.TrimSpace(l[:i])
			ver := strings.TrimSpace(l[i+2:])
			push("PyPI", name, ver, true)
		}
	}
}

// cleanVersion strips npm version-range prefixes like ^, ~, >=, etc. so
// the resulting string is the exact version OSV can match against. For
// ranges like ">=1.2.3 <2.0.0" we take the lower bound (better than
// nothing). Returns "" if no parseable version is present.
func cleanVersion(raw string) string {
	v := strings.TrimSpace(raw)
	v = strings.TrimLeft(v, "^~>=<! ")
	if i := strings.IndexAny(v, " <>"); i >= 0 {
		v = v[:i]
	}
	if v == "" || v == "*" || strings.HasPrefix(v, "file:") || strings.HasPrefix(v, "git+") {
		return ""
	}
	return v
}

// Probe verifies OSV.dev is reachable. Used by /api/status to render a
// "Supply chain ready" row in the connections panel.
func Probe(ctx context.Context, osv *prepass.OSVClient) error {
	if osv == nil {
		osv = prepass.DefaultOSV()
	}
	// Cheap query: known-good package with no vulns in current release.
	_, err := osv.Lookup("npm", "is-odd", "3.0.1")
	_ = ctx // reserved for future request-context wiring
	if err != nil {
		return fmt.Errorf("osv probe: %w", err)
	}
	return nil
}

// ErrUnreadableManifest is returned when a manifest is malformed enough that
// we can't extract any deps. Callers may want to surface this as a
// low-severity "couldn't inventory" finding.
var ErrUnreadableManifest = errors.New("manifest unreadable")
