package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/chawdamrunal/assay/internal/prepass"
)

// OSVTool wraps prepass.OSVClient as an agent-callable tool.
type OSVTool struct {
	client *prepass.OSVClient
}

// NewOSVTool returns a new OSVTool.
func NewOSVTool(client *prepass.OSVClient) *OSVTool { return &OSVTool{client: client} }

// Def returns the agent-facing tool definition.
func (o *OSVTool) Def() Tool {
	return Tool{
		Name:        "osv_lookup",
		Description: "Look up known CVEs for a specific package version against OSV.dev. Returns a JSON list of vulnerabilities (id, summary, severity) or an empty list if none.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"ecosystem": map[string]any{"type": "string", "description": "e.g. npm, PyPI, Go, crates.io"},
				"package":   map[string]any{"type": "string"},
				"version":   map[string]any{"type": "string"},
			},
			"required": []string{"ecosystem", "package", "version"},
		},
	}
}

// Lookup executes the tool.
func (o *OSVTool) Lookup(_ context.Context, in Invocation) (Result, error) {
	eco, _ := in.Input["ecosystem"].(string)
	pkg, _ := in.Input["package"].(string)
	ver, _ := in.Input["version"].(string)
	if eco == "" || pkg == "" || ver == "" {
		return Result{}, errors.New("osv_lookup: ecosystem, package, and version are all required")
	}
	hits, err := o.client.Lookup(eco, pkg, ver)
	if err != nil {
		return Result{}, fmt.Errorf("osv_lookup: %w", err)
	}
	out, _ := json.MarshalIndent(hits, "", "  ")
	return Result{Text: string(out)}, nil
}
