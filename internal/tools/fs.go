package tools

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// FS bundles the filesystem tools (read_file, list_dir, grep) bounded to a root.
type FS struct {
	root string
}

// NewFS returns an FS rooted at root. All operations are bounded under it.
func NewFS(root string) *FS {
	abs, _ := filepath.Abs(root)
	return &FS{root: abs}
}

// Defs returns the agent-facing tool definitions.
func (f *FS) Defs() []Tool {
	return []Tool{
		{
			Name:        "read_file",
			Description: "Read a file relative to the scan root. Optionally restrict to a line range. Returns up to 200 lines per call.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":       map[string]any{"type": "string", "description": "Path relative to the target root."},
					"start_line": map[string]any{"type": "integer", "minimum": 1},
					"end_line":   map[string]any{"type": "integer", "minimum": 1},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "list_dir",
			Description: "List entries in a directory under the target root. Subdirectories are suffixed with /.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string"},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "grep",
			Description: "Search for a regex pattern across all readable text files under the target root. Returns up to 50 matching lines with file:line prefixes.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pattern": map[string]any{"type": "string"},
					"path":    map[string]any{"type": "string", "description": "Optional sub-path to limit search."},
				},
				"required": []string{"pattern"},
			},
		},
		{
			Name:        "symbol_refs",
			Description: "Find where a symbol (function / variable / type name) is defined and every place it is referenced under the target, in one call — definitions first, then references, as file:line. Use this to trace data flow (a value's source to its sinks) instead of chaining grep and read_file.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"symbol": map[string]any{"type": "string", "description": "Identifier to locate (e.g. a function or variable name)."},
					"path":   map[string]any{"type": "string", "description": "Optional sub-path to limit the search."},
				},
				"required": []string{"symbol"},
			},
		},
	}
}

func (f *FS) resolve(rel string) (string, error) {
	clean := filepath.Clean(rel)
	if filepath.IsAbs(clean) {
		return "", errors.New("absolute paths not allowed")
	}
	full := filepath.Join(f.root, clean)
	abs, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(abs+string(filepath.Separator), f.root+string(filepath.Separator)) && abs != f.root {
		return "", fmt.Errorf("path escapes root: %s", rel)
	}
	return abs, nil
}

func pathArg(in map[string]any) string {
	if v, ok := in["path"].(string); ok {
		return v
	}
	return "."
}

func intArg(in map[string]any, key string) int {
	switch v := in[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}

// ReadFile reads a (sub-range of a) file.
func (f *FS) ReadFile(_ context.Context, in Invocation) (Result, error) {
	path := pathArg(in.Input)
	full, err := f.resolve(path)
	if err != nil {
		return Result{}, err
	}
	// Guard against loading a huge file entirely into memory before the line
	// cap discards most of it. A scan target may bundle a multi-megabyte blob
	// (WASM, vendored .pack, model weights); os.ReadFile would pull all of it
	// onto the heap. The grep tool applies a similar size guard; here we use a
	// generous 10 MiB ceiling since legitimate source files are tiny. Reading
	// a sub-range still needs the whole file in this implementation, so the
	// guard applies regardless of start_line/end_line.
	const maxReadBytes = 10 << 20 // 10 MiB
	if info, statErr := os.Stat(full); statErr == nil && info.Size() > maxReadBytes {
		return Result{Text: fmt.Sprintf("// %s: file too large to read (%d bytes, limit %d); use grep to locate content within it", path, info.Size(), maxReadBytes)}, nil
	}
	data, err := os.ReadFile(full) // #nosec G304 -- bounded under f.root by resolve()
	if err != nil {
		return Result{}, fmt.Errorf("read_file %s: %w", path, err)
	}
	start := intArg(in.Input, "start_line")
	end := intArg(in.Input, "end_line")
	if start == 0 && end == 0 {
		return Result{Text: capLines(string(data), 1, 200, path)}, nil
	}
	if start == 0 {
		start = 1
	}
	if end == 0 {
		end = start + 200
	}
	if end-start > 200 {
		end = start + 200
	}
	return Result{Text: capLines(string(data), start, end, path)}, nil
}

func capLines(content string, start, end int, path string) string {
	lines := strings.Split(content, "\n")
	if start < 1 {
		start = 1
	}
	if end > len(lines) {
		end = len(lines)
	}
	if start > len(lines) {
		return fmt.Sprintf("// %s: requested lines %d-%d, file has %d lines", path, start, end, len(lines))
	}
	var b strings.Builder
	for i := start - 1; i < end; i++ {
		fmt.Fprintf(&b, "%s:%d: %s\n", path, i+1, lines[i])
	}
	return b.String()
}

// ListDir returns a sorted list of entries in dir.
func (f *FS) ListDir(_ context.Context, in Invocation) (Result, error) {
	full, err := f.resolve(pathArg(in.Input))
	if err != nil {
		return Result{}, err
	}
	entries, err := os.ReadDir(full)
	if err != nil {
		return Result{}, fmt.Errorf("list_dir: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return Result{Text: strings.Join(names, "\n")}, nil
}

// Grep walks the root (or sub-path) and returns regex matches.
func (f *FS) Grep(_ context.Context, in Invocation) (Result, error) {
	patStr, _ := in.Input["pattern"].(string)
	if patStr == "" {
		return Result{}, errors.New("grep: empty pattern")
	}
	pat, err := regexp.Compile(patStr)
	if err != nil {
		return Result{}, fmt.Errorf("grep: invalid pattern: %w", err)
	}
	subpath := pathArg(in.Input)
	full, err := f.resolve(subpath)
	if err != nil {
		return Result{}, err
	}
	const maxMatches = 50
	var lines []string
	err = filepath.WalkDir(full, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		base := filepath.Base(path)
		if d.IsDir() {
			// Skip dotdirs and dependency trees — grepping node_modules/vendor
			// both blows the match cap on bundled third-party code and pushes
			// the plugin's own (interesting) files past the cap, silently
			// hiding them. SCA already inventories dependencies separately.
			if strings.HasPrefix(base, ".") || base == "node_modules" || base == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(base, ".") {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > 1<<20 {
			return nil
		}
		fileLines, err := grepFile(path, pat, f.root, maxMatches-len(lines))
		if err != nil {
			return nil
		}
		lines = append(lines, fileLines...)
		if len(lines) >= maxMatches {
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return Result{}, fmt.Errorf("grep walk: %w", err)
	}
	if len(lines) == 0 {
		return Result{Text: "(no matches)"}, nil
	}
	out := strings.Join(lines, "\n")
	if len(lines) >= maxMatches {
		// Signal truncation so the caller knows the result is partial and can
		// re-run with a narrower `path`, instead of treating 50 as "all".
		out += fmt.Sprintf("\n[TRUNCATED: returned the first %d matches; narrow the search with a more specific `path` to see the rest]", maxMatches)
	}
	return Result{Text: out}, nil
}

// SymbolRefs locates a symbol's definitions and references under the target in
// a single call, so the model can trace data flow (where a value comes from,
// where it goes) without chaining grep + read_file and burning its turn budget.
//
// Classification is heuristic and language-agnostic: a hit is a "definition"
// when a declaration keyword precedes the symbol (func/def/fn/function/class/
// type/interface/struct/enum/const/let/var) or the symbol is assigned to
// (`=` / `:=`, but not `==`); everything else is a "reference". Skips
// node_modules/vendor/dotdirs and files over 1 MiB, and caps total hits.
func (f *FS) SymbolRefs(_ context.Context, in Invocation) (Result, error) {
	symbol := strings.TrimSpace(symbolArg(in.Input))
	if symbol == "" {
		return Result{}, errors.New("symbol_refs: empty symbol")
	}
	full, err := f.resolve(pathArg(in.Input))
	if err != nil {
		return Result{}, err
	}
	qs := regexp.QuoteMeta(symbol)
	wordRe, err := regexp.Compile(`\b` + qs + `\b`)
	if err != nil {
		return Result{}, fmt.Errorf("symbol_refs: %w", err)
	}
	// Declaration keyword before the symbol, OR assignment (`=`/`:=`, not `==`).
	defRe := regexp.MustCompile(`\b(?:func|def|fn|function|class|type|interface|struct|enum|const|let|var)\s+` + qs + `\b|\b` + qs + `\s*:?=(?:[^=]|$)`)

	const maxHits = 60
	var defs, refs []string
	total := 0
	truncated := false
	err = filepath.WalkDir(full, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		base := filepath.Base(path)
		if d.IsDir() {
			if strings.HasPrefix(base, ".") || base == "node_modules" || base == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(base, ".") {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil || info.Size() > 1<<20 {
			return nil
		}
		ff, oerr := os.Open(path) // #nosec G304 G122 -- WalkDir bounded under f.root
		if oerr != nil {
			return nil
		}
		rel, _ := filepath.Rel(f.root, path)
		sc := bufio.NewScanner(ff)
		sc.Buffer(make([]byte, 64*1024), 1<<20)
		ln := 0
		for sc.Scan() {
			ln++
			text := sc.Text()
			if !wordRe.MatchString(text) {
				continue
			}
			if total >= maxHits {
				truncated = true
				break
			}
			entry := fmt.Sprintf("%s:%d: %s", rel, ln, strings.TrimSpace(text))
			if defRe.MatchString(text) {
				defs = append(defs, entry)
			} else {
				refs = append(refs, entry)
			}
			total++
		}
		_ = ff.Close()
		if total >= maxHits {
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return Result{}, fmt.Errorf("symbol_refs walk: %w", err)
	}
	if total == 0 {
		return Result{Text: fmt.Sprintf("(no references to %q found under the target)", symbol)}, nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Definitions of %q (%d):\n", symbol, len(defs))
	if len(defs) == 0 {
		b.WriteString("  (none found — the symbol may be imported or external)\n")
	}
	for _, d := range defs {
		b.WriteString("  " + d + "\n")
	}
	fmt.Fprintf(&b, "References to %q (%d):\n", symbol, len(refs))
	for _, r := range refs {
		b.WriteString("  " + r + "\n")
	}
	if truncated {
		fmt.Fprintf(&b, "[TRUNCATED: showed the first %d hits; narrow with `path` to see the rest]\n", maxHits)
	}
	return Result{Text: b.String()}, nil
}

func symbolArg(in map[string]any) string {
	if v, ok := in["symbol"].(string); ok {
		return v
	}
	return ""
}

func grepFile(path string, pat *regexp.Regexp, root string, remaining int) ([]string, error) {
	if remaining <= 0 {
		return nil, nil
	}
	f, err := os.Open(path) // #nosec G304 -- WalkDir bounded
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	rel, _ := filepath.Rel(root, path)
	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	line := 0
	for sc.Scan() && len(out) < remaining {
		line++
		if pat.MatchString(sc.Text()) {
			out = append(out, fmt.Sprintf("%s:%d: %s", rel, line, sc.Text()))
		}
	}
	return out, nil
}
