package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Manifest parses plugin/MCP/package manifests into a normalized JSON shape.
type Manifest struct {
	root string
}

// NewManifest returns a Manifest bounded to root.
func NewManifest(root string) *Manifest {
	abs, _ := filepath.Abs(root)
	return &Manifest{root: abs}
}

// Def returns the agent-facing tool definition.
func (m *Manifest) Def() Tool {
	return Tool{
		Name:        "parse_manifest",
		Description: "Parse a manifest file (plugin.json, package.json, manifest.json, pyproject.toml, go.mod) and return a normalized JSON structure with name, version, kind, declared capabilities, and dependencies.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "Path relative to the target root."},
			},
			"required": []string{"path"},
		},
	}
}

// Parse executes the parse_manifest tool.
func (m *Manifest) Parse(_ context.Context, in Invocation) (Result, error) {
	relPath, _ := in.Input["path"].(string)
	if relPath == "" {
		return Result{}, errors.New("parse_manifest: empty path")
	}
	full, err := m.resolve(relPath)
	if err != nil {
		return Result{}, err
	}
	data, err := os.ReadFile(full) // #nosec G304 -- bounded under root by resolve
	if err != nil {
		return Result{}, fmt.Errorf("parse_manifest read: %w", err)
	}

	base := filepath.Base(full)
	var parsed map[string]any
	switch base {
	case "plugin.json":
		parsed, err = parseGenericJSON(data)
		if err == nil {
			parsed["kind"] = "claude-code-plugin"
		}
	case "package.json":
		parsed, err = parseGenericJSON(data)
		if err == nil {
			parsed["kind"] = "npm-package"
		}
	case "manifest.json":
		parsed, err = parseGenericJSON(data)
		if err == nil {
			parsed["kind"] = "mcp-server"
		}
	case "pyproject.toml":
		parsed, err = parsePyprojectTOML(data)
		if err == nil {
			parsed["kind"] = "python-project"
		}
	case "go.mod":
		parsed, err = parseGoMod(data)
		if err == nil {
			parsed["kind"] = "go-module"
		}
	default:
		return Result{}, fmt.Errorf("parse_manifest: unknown manifest filename %q (supported: plugin.json, package.json, manifest.json, pyproject.toml, go.mod)", base)
	}
	if err != nil {
		return Result{}, fmt.Errorf("parse_manifest %s: %w", base, err)
	}

	out, err := json.MarshalIndent(parsed, "", "  ")
	if err != nil {
		return Result{}, fmt.Errorf("parse_manifest encode: %w", err)
	}
	return Result{Text: string(out)}, nil
}

func parseGenericJSON(data []byte) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// parsePyprojectTOML extracts a narrow set of fields from pyproject.toml.
// We don't pull in a TOML parser for just this; we regex the common keys.
func parsePyprojectTOML(data []byte) (map[string]any, error) {
	text := string(data)
	out := map[string]any{}

	nameRe := regexp.MustCompile(`(?m)^name\s*=\s*"([^"]+)"`)
	versionRe := regexp.MustCompile(`(?m)^version\s*=\s*"([^"]+)"`)
	descRe := regexp.MustCompile(`(?m)^description\s*=\s*"([^"]+)"`)

	if m := nameRe.FindStringSubmatch(text); m != nil {
		out["name"] = m[1]
	}
	if m := versionRe.FindStringSubmatch(text); m != nil {
		out["version"] = m[1]
	}
	if m := descRe.FindStringSubmatch(text); m != nil {
		out["description"] = m[1]
	}
	return out, nil
}

// parseGoMod extracts the module path and require block from a go.mod.
func parseGoMod(data []byte) (map[string]any, error) {
	text := string(data)
	out := map[string]any{}

	moduleRe := regexp.MustCompile(`(?m)^module\s+(\S+)`)
	goVerRe := regexp.MustCompile(`(?m)^go\s+(\S+)`)
	if m := moduleRe.FindStringSubmatch(text); m != nil {
		out["name"] = m[1]
	}
	if m := goVerRe.FindStringSubmatch(text); m != nil {
		out["go_version"] = m[1]
	}

	// Very light "require" extraction (single-line and block forms).
	deps := map[string]string{}
	requireBlockRe := regexp.MustCompile(`(?s)require\s*\(([^)]+)\)`)
	if m := requireBlockRe.FindStringSubmatch(text); m != nil {
		for _, line := range strings.Split(m[1], "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "//") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				deps[fields[0]] = fields[1]
			}
		}
	}
	requireLineRe := regexp.MustCompile(`(?m)^require\s+(\S+)\s+(\S+)`)
	for _, m := range requireLineRe.FindAllStringSubmatch(text, -1) {
		deps[m[1]] = m[2]
	}
	if len(deps) > 0 {
		out["dependencies"] = deps
	}
	return out, nil
}

func (m *Manifest) resolve(rel string) (string, error) {
	clean := filepath.Clean(rel)
	if filepath.IsAbs(clean) {
		return "", errors.New("absolute paths not allowed")
	}
	full := filepath.Join(m.root, clean)
	abs, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(abs+string(filepath.Separator), m.root+string(filepath.Separator)) && abs != m.root {
		return "", fmt.Errorf("path escapes root: %s", rel)
	}
	return abs, nil
}
