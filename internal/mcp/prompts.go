package mcp

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

//go:embed methodology.md
var methodologyTemplate string

// registerMethodologyPrompt exposes the 5-stage Assay security-review playbook
// as an MCP prompt. Claude Code's /assay-scan slash command loads this prompt
// with a target path; Claude then drives the scan by chaining assay_* tool
// calls instead of the Go orchestrator doing it.
//
// The prompt is intentionally prescriptive about the SEQUENCE (start →
// methodology → finalize) because the disk format depends on it: events.jsonl
// is opened by assay_scan_start, and assay_finalize_scan reads back the
// findings.jsonl that record_finding appended.
func registerMethodologyPrompt(s *server.MCPServer) {
	p := mcp.NewPrompt("assay_methodology",
		mcp.WithPromptDescription("The Assay 5-stage security-review methodology. Invoke with a target (absolute path) to scan that artifact end-to-end via the assay_* tools. Produces audit.json + audit.md under ~/.assay/scans/<target>/<scan_id>/."),
		mcp.WithArgument("target",
			mcp.ArgumentDescription("Absolute path to the artifact (Claude Code plugin, MCP server, or local directory) to scan."),
			mcp.RequiredArgument(),
		),
	)
	s.AddPrompt(p, func(_ context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		target, ok := req.Params.Arguments["target"]
		if !ok || target == "" {
			return nil, fmt.Errorf("target argument is required")
		}
		body := strings.ReplaceAll(methodologyTemplate, "{{TARGET}}", target)
		return &mcp.GetPromptResult{
			Description: "Assay methodology v1",
			Messages: []mcp.PromptMessage{
				{
					Role:    mcp.RoleUser,
					Content: mcp.NewTextContent(body),
				},
			},
		}, nil
	})
}
