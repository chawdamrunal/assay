// Package mcp exposes Assay's scanner primitives as a Model Context Protocol
// server. The MCP entry point is the default scan architecture: Claude Code
// (or any MCP-compatible client) drives the scan, calling our bounded tools
// to read the target, record findings, and finalize a verdict.
//
// The tool surface is split into read-only filesystem + analysis primitives
// (assay_list_files, assay_read_file, assay_grep, assay_parse_manifest,
// assay_osv_lookup, assay_secret_scan) and the scan-state lifecycle
// (assay_scan_start, assay_record_finding, assay_finalize_scan,
// assay_emit_progress).
package mcp

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/chawdamrunal/assay/internal/tools"
)

// Version of the MCP server shown to clients in initialize responses.
const Version = "0.3.0-dev"

// NewServer constructs the Assay MCP server with the read-only tool set
// only. Use NewServerWithState when you want the scan-state mutators
// (scan_start / record_finding / emit_progress / finalize_scan), which need
// somewhere to persist scans.
//
// Kept as a separate entry point for tests + ad-hoc usage that doesn't need
// a scans directory.
func NewServer() *server.MCPServer {
	s := server.NewMCPServer(
		"assay",
		Version,
		server.WithToolCapabilities(true),
		server.WithResourceCapabilities(true, true),
		server.WithPromptCapabilities(true),
	)

	registerListFiles(s)
	registerReadFile(s)
	registerReadOnlyTools(s)
	registerMethodologyPrompt(s)
	return s
}

// NewServerWithState constructs an MCP server with the full tool surface —
// read-only investigation tools plus the four scan-state mutators that
// drive an end-to-end scan. scansDir is typically ~/.assay/scans.
func NewServerWithState(scansDir string) *server.MCPServer {
	return NewServerWithStateOffline(scansDir, false)
}

// NewServerWithStateOffline is NewServerWithState with the offline flag wired
// into the scan state, so the deterministic SCA floor at finalize time skips
// the OSV.dev network lookup for an air-gapped scan.
func NewServerWithStateOffline(scansDir string, offline bool) *server.MCPServer {
	s := NewServer()
	registerScanStateTools(s, NewScanStateWithOffline(scansDir, offline))
	return s
}

// registerListFiles exposes the bounded list_dir primitive as the MCP tool
// `assay_list_files`. A scan target is provided per call so the same MCP
// server can serve scans against multiple roots concurrently.
func registerListFiles(s *server.MCPServer) {
	tool := mcp.NewTool("assay_list_files",
		mcp.WithDescription("List the entries in a directory under the scan target. Subdirectories are suffixed with /."),
		mcp.WithString("target",
			mcp.Description("Absolute path to the scan target root."),
			mcp.Required(),
		),
		mcp.WithString("path",
			mcp.Description("Path under the target root. Defaults to . (the root itself)."),
		),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		target, ok := args["target"].(string)
		if !ok || target == "" {
			return mcp.NewToolResultError("target is required"), nil
		}
		if !filepath.IsAbs(target) {
			return mcp.NewToolResultError("target must be an absolute path"), nil
		}
		sub := "."
		if v, ok := args["path"].(string); ok && v != "" {
			sub = v
		}
		fs := tools.NewFS(target)
		res, err := fs.ListDir(ctx, tools.Invocation{
			Name:  "list_dir",
			Input: map[string]any{"path": sub},
		})
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(res.Text), nil
	})
}

// registerReadFile exposes the bounded read_file primitive as `assay_read_file`.
// Reads are capped at 200 lines per call to keep agent contexts manageable; the
// caller supplies start_line/end_line for chunked reads of larger files.
func registerReadFile(s *server.MCPServer) {
	tool := mcp.NewTool("assay_read_file",
		mcp.WithDescription("Read a file under the scan target. Optionally restrict to a line range. Returns up to 200 lines per call, each prefixed with path:line."),
		mcp.WithString("target",
			mcp.Description("Absolute path to the scan target root."),
			mcp.Required(),
		),
		mcp.WithString("path",
			mcp.Description("Path under the target root."),
			mcp.Required(),
		),
		mcp.WithNumber("start_line",
			mcp.Description("Optional 1-indexed inclusive start line."),
		),
		mcp.WithNumber("end_line",
			mcp.Description("Optional 1-indexed inclusive end line (capped to start_line + 200)."),
		),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		target, ok := args["target"].(string)
		if !ok || target == "" {
			return mcp.NewToolResultError("target is required"), nil
		}
		if !filepath.IsAbs(target) {
			return mcp.NewToolResultError("target must be an absolute path"), nil
		}
		path, ok := args["path"].(string)
		if !ok || path == "" {
			return mcp.NewToolResultError("path is required"), nil
		}
		input := map[string]any{"path": path}
		if v, ok := args["start_line"].(float64); ok {
			input["start_line"] = v
		}
		if v, ok := args["end_line"].(float64); ok {
			input["end_line"] = v
		}
		fs := tools.NewFS(target)
		res, err := fs.ReadFile(ctx, tools.Invocation{Name: "read_file", Input: input})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("assay_read_file: %v", err)), nil
		}
		return mcp.NewToolResultText(res.Text), nil
	})
}
