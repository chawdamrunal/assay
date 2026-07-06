package mcp

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/chawdamrunal/assay/internal/prepass"
	"github.com/chawdamrunal/assay/internal/tools"
)

// registerReadOnlyTools adds the remaining read-only investigation tools that
// Claude needs to walk a target: grep, parse_manifest, osv_lookup, secret_scan.
func registerReadOnlyTools(s *server.MCPServer) {
	registerGrep(s)
	registerSymbolRefs(s)
	registerParseManifest(s)
	registerOSVLookup(s)
	registerSecretScan(s)
}

func registerGrep(s *server.MCPServer) {
	tool := mcp.NewTool("assay_grep",
		mcp.WithDescription("Search for a regex pattern across text files under the scan target. Returns up to 50 matching lines, each prefixed file:line."),
		mcp.WithString("target", mcp.Description("Absolute path to the scan target root."), mcp.Required()),
		mcp.WithString("pattern", mcp.Description("Go regexp (RE2) pattern."), mcp.Required()),
		mcp.WithString("path", mcp.Description("Optional sub-path under the target to limit the search.")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		target, ok := args["target"].(string)
		if !ok || !filepath.IsAbs(target) {
			return mcp.NewToolResultError("target must be an absolute path"), nil
		}
		pat, _ := args["pattern"].(string)
		if pat == "" {
			return mcp.NewToolResultError("pattern is required"), nil
		}
		sub := "."
		if v, ok := args["path"].(string); ok && v != "" {
			sub = v
		}
		fs := tools.NewFS(target)
		res, err := fs.Grep(ctx, tools.Invocation{
			Name:  "grep",
			Input: map[string]any{"pattern": pat, "path": sub},
		})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("assay_grep: %v", err)), nil
		}
		return mcp.NewToolResultText(res.Text), nil
	})
}

func registerSymbolRefs(s *server.MCPServer) {
	tool := mcp.NewTool("assay_symbol_refs",
		mcp.WithDescription("Find where a symbol (function/variable/type name) is defined and every place it is referenced under the target, in one call — definitions then references, as file:line. Use it to trace data flow (a value's source to its sinks) without chaining grep and read_file."),
		mcp.WithString("target", mcp.Description("Absolute path to the scan target root."), mcp.Required()),
		mcp.WithString("symbol", mcp.Description("Identifier to locate (function/variable/type name)."), mcp.Required()),
		mcp.WithString("path", mcp.Description("Optional sub-path under the target to limit the search.")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		target, ok := args["target"].(string)
		if !ok || !filepath.IsAbs(target) {
			return mcp.NewToolResultError("target must be an absolute path"), nil
		}
		sym, _ := args["symbol"].(string)
		if sym == "" {
			return mcp.NewToolResultError("symbol is required"), nil
		}
		sub := "."
		if v, ok := args["path"].(string); ok && v != "" {
			sub = v
		}
		fs := tools.NewFS(target)
		res, err := fs.SymbolRefs(ctx, tools.Invocation{
			Name:  "symbol_refs",
			Input: map[string]any{"symbol": sym, "path": sub},
		})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("assay_symbol_refs: %v", err)), nil
		}
		return mcp.NewToolResultText(res.Text), nil
	})
}

func registerParseManifest(s *server.MCPServer) {
	tool := mcp.NewTool("assay_parse_manifest",
		mcp.WithDescription("Parse a known manifest file under the target: package.json, plugin.json, manifest.json, pyproject.toml, go.mod. Returns a JSON structure of the declared dependencies / permissions / metadata."),
		mcp.WithString("target", mcp.Description("Absolute path to the scan target root."), mcp.Required()),
		mcp.WithString("path", mcp.Description("Path under the target root to the manifest file."), mcp.Required()),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		target, ok := args["target"].(string)
		if !ok || !filepath.IsAbs(target) {
			return mcp.NewToolResultError("target must be an absolute path"), nil
		}
		path, _ := args["path"].(string)
		if path == "" {
			return mcp.NewToolResultError("path is required"), nil
		}
		m := tools.NewManifest(target)
		res, err := m.Parse(ctx, tools.Invocation{Name: "parse_manifest", Input: map[string]any{"path": path}})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("assay_parse_manifest: %v", err)), nil
		}
		return mcp.NewToolResultText(res.Text), nil
	})
}

func registerOSVLookup(s *server.MCPServer) {
	tool := mcp.NewTool("assay_osv_lookup",
		mcp.WithDescription("Query the OSV.dev vulnerability database for a single package + version. Returns advisories (ID, summary, severity) or 'no known vulnerabilities'. Network call — skip in --offline runs."),
		mcp.WithString("ecosystem", mcp.Description("OSV ecosystem (npm, PyPI, Go, etc)."), mcp.Required()),
		mcp.WithString("package", mcp.Description("Package name."), mcp.Required()),
		mcp.WithString("version", mcp.Description("Exact version string."), mcp.Required()),
	)
	osvClient := prepass.DefaultOSV()
	osvTool := tools.NewOSVTool(osvClient)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		eco, _ := args["ecosystem"].(string)
		pkg, _ := args["package"].(string)
		ver, _ := args["version"].(string)
		if eco == "" || pkg == "" || ver == "" {
			return mcp.NewToolResultError("ecosystem, package, and version are required"), nil
		}
		res, err := osvTool.Lookup(ctx, tools.Invocation{
			Name:  "osv_lookup",
			Input: map[string]any{"ecosystem": eco, "package": pkg, "version": ver},
		})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("assay_osv_lookup: %v", err)), nil
		}
		return mcp.NewToolResultText(res.Text), nil
	})
}

func registerSecretScan(s *server.MCPServer) {
	tool := mcp.NewTool("assay_secret_scan",
		mcp.WithDescription("Run the regex + entropy secret scanner under the scan target (or a sub-path). Returns 'file:line: <secret type> hit' lines, or 'no secrets found'."),
		mcp.WithString("target", mcp.Description("Absolute path to the scan target root."), mcp.Required()),
		mcp.WithString("path", mcp.Description("Optional sub-path under the target.")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		target, ok := args["target"].(string)
		if !ok || !filepath.IsAbs(target) {
			return mcp.NewToolResultError("target must be an absolute path"), nil
		}
		sub := "."
		if v, ok := args["path"].(string); ok && v != "" {
			sub = v
		}
		secrets := tools.NewSecretsTool(target)
		res, err := secrets.Scan(ctx, tools.Invocation{Name: "secret_scan", Input: map[string]any{"path": sub}})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("assay_secret_scan: %v", err)), nil
		}
		return mcp.NewToolResultText(res.Text), nil
	})
}
