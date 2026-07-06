package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newStatefulTestClient returns a client connected to a server with the full
// tool set including the scan-state mutators backed by a per-test scansDir.
func newStatefulTestClient(t *testing.T) (*mcpclient.Client, string) {
	t.Helper()
	scansDir := t.TempDir()
	s := NewServerWithState(scansDir)
	c := connect(t, s)
	return c, scansDir
}

func connect(t *testing.T, s *server.MCPServer) *mcpclient.Client {
	t.Helper()
	c, err := mcpclient.NewInProcessClient(s)
	require.NoError(t, err)
	require.NoError(t, c.Start(context.Background()))
	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: "test", Version: "0"}
	_, err = c.Initialize(context.Background(), initReq)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// TestDeriveTargetNameInstallPath asserts the cache-layout fix: a plugin
// install path like ~/.claude/plugins/cache/<m>/<name>/<v> must derive
// <name>, not <v>. Multiple plugins sharing a version would otherwise
// collide in ~/.assay/scans/<v>/.
func TestDeriveTargetNameInstallPath(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"/Users/u/.claude/plugins/cache/claude-plugins-official/clangd-lsp/1.0.0", "clangd-lsp"},
		{"/Users/u/.claude/plugins/cache/marketplace/frontend-design/3a92c028770f", "frontend-design"},
		{"/Users/u/.claude/plugins/cache/m/superpowers/5.1.0/", "superpowers"},
		// Arbitrary target dirs keep filepath.Base semantics.
		{"/tmp/some-fixture", "some-fixture"},
		{"/Users/u/Downloads/assay/testdata/corpus/safe/rainbow-formatter", "rainbow-formatter"},
		// Edge: cache layout but truncated — fall back to basename.
		{"/Users/u/.claude/plugins/cache", "cache"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, deriveTargetName(c.in), "in=%q", c.in)
	}
}

func TestAllocateAsOverridesTargetName(t *testing.T) {
	st := NewScanState(t.TempDir())
	cachePath := "/Users/u/.claude/plugins/cache/m/clangd-lsp/1.0.0"
	dir, err := st.AllocateAs("scan-1", cachePath, "clangd-lsp")
	require.NoError(t, err)
	assert.Contains(t, dir, "/clangd-lsp/scan-1")
	assert.NotContains(t, dir, "/1.0.0/")
}

func TestStatefulServerExposesAllTools(t *testing.T) {
	c, _ := newStatefulTestClient(t)
	resp, err := c.ListTools(context.Background(), mcp.ListToolsRequest{})
	require.NoError(t, err)
	names := map[string]bool{}
	for _, t := range resp.Tools {
		names[t.Name] = true
	}
	for _, want := range []string{
		"assay_list_files", "assay_read_file", "assay_grep",
		"assay_parse_manifest", "assay_osv_lookup", "assay_secret_scan",
		"assay_scan_start", "assay_emit_progress", "assay_record_finding",
		"assay_finalize_scan",
	} {
		assert.True(t, names[want], "missing tool %s", want)
	}
}

// TestEndToEndScanFlow runs the exact sequence a Claude agent would: start a
// scan, emit progress, record an evidence-backed finding, finalize. Asserts
// the on-disk audit.json + audit.md materialize and the citation validator
// kept the finding (because the snippet really exists in the target file).
func TestEndToEndScanFlow(t *testing.T) {
	target := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(target, "main.js"),
		[]byte("line one\nfs.readFileSync('/etc/passwd')\nline three\n"),
		0o600,
	))

	c, scansDir := newStatefulTestClient(t)
	ctx := context.Background()

	// 1. scan_start
	startResp, err := c.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "assay_scan_start",
			Arguments: map[string]any{"target": target},
		},
	})
	require.NoError(t, err)
	require.False(t, startResp.IsError, "start error: %s", textOf(t, startResp))
	var startBody struct {
		ScanID  string `json:"scan_id"`
		ScanDir string `json:"scan_dir"`
	}
	require.NoError(t, json.Unmarshal([]byte(textOf(t, startResp)), &startBody))
	assert.NotEmpty(t, startBody.ScanID)
	assert.True(t, strings.HasPrefix(startBody.ScanDir, scansDir), "scan dir under scansDir")

	// 2. emit_progress
	progResp, err := c.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "assay_emit_progress",
			Arguments: map[string]any{
				"scan_id": startBody.ScanID,
				"stage":   "investigation",
				"status":  "start",
				"message": "investigating credential exfil",
			},
		},
	})
	require.NoError(t, err)
	require.False(t, progResp.IsError)

	// 3. record_finding with a real snippet from the target file
	findResp, err := c.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "assay_record_finding",
			Arguments: map[string]any{
				"scan_id": startBody.ScanID,
				"finding": map[string]any{
					"id":       "F1",
					"severity": "critical",
					"category": "exfiltration",
					"title":    "Reads /etc/passwd at startup",
					"evidence": []any{
						map[string]any{
							"file":    "main.js",
							"line":    float64(2),
							"snippet": "fs.readFileSync('/etc/passwd')",
						},
					},
					"exploit_scenario": "Anyone installing this plugin leaks the user's passwd file.",
				},
			},
		},
	})
	require.NoError(t, err)
	require.False(t, findResp.IsError, "record_finding error: %s", textOf(t, findResp))

	// 4. finalize_scan
	finResp, err := c.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "assay_finalize_scan",
			Arguments: map[string]any{
				"scan_id":      startBody.ScanID,
				"target":       target,
				"verdict":      "unsafe",
				"summary":      "Plugin reads /etc/passwd.",
				"threat_model": "### T1 Credential exfil",
			},
		},
	})
	require.NoError(t, err)
	require.False(t, finResp.IsError, "finalize error: %s", textOf(t, finResp))

	// Audit on disk
	auditBytes, err := os.ReadFile(filepath.Join(startBody.ScanDir, "audit.json"))
	require.NoError(t, err)
	var audit map[string]any
	require.NoError(t, json.Unmarshal(auditBytes, &audit))
	assert.Equal(t, "unsafe", audit["verdict"])
	findings, _ := audit["findings"].([]any)
	require.Len(t, findings, 1, "validated finding should survive")
	assert.Equal(t, "F1", findings[0].(map[string]any)["id"])

	// audit.md is non-empty
	md, err := os.ReadFile(filepath.Join(startBody.ScanDir, "audit.md"))
	require.NoError(t, err)
	assert.Contains(t, string(md), "Reads /etc/passwd")
}

// TestFinalizeDropsFakeCitation asserts the hard-quote validator strips
// findings whose evidence snippet does not appear in the cited file. This
// is the architectural anti-hallucination guarantee — must work via MCP.
func TestFinalizeDropsFakeCitation(t *testing.T) {
	target := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(target, "ok.js"), []byte("console.log('hi')\n"), 0o600))

	c, _ := newStatefulTestClient(t)
	ctx := context.Background()

	startResp, _ := c.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "assay_scan_start",
			Arguments: map[string]any{"target": target},
		},
	})
	var sb struct {
		ScanID  string `json:"scan_id"`
		ScanDir string `json:"scan_dir"`
	}
	_ = json.Unmarshal([]byte(textOf(t, startResp)), &sb)

	// Fabricated snippet that does NOT appear in ok.js.
	_, _ = c.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "assay_record_finding",
			Arguments: map[string]any{
				"scan_id": sb.ScanID,
				"finding": map[string]any{
					"id":       "FAKE",
					"severity": "critical",
					"category": "exfiltration",
					"title":    "Claim with bogus evidence",
					"evidence": []any{
						map[string]any{
							"file":    "ok.js",
							"line":    float64(1),
							"snippet": "fs.readFileSync('/etc/shadow')",
						},
					},
				},
			},
		},
	})

	_, _ = c.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "assay_finalize_scan",
			Arguments: map[string]any{
				"scan_id": sb.ScanID,
				"target":  target,
				"verdict": "unsafe",
			},
		},
	})

	auditBytes, err := os.ReadFile(filepath.Join(sb.ScanDir, "audit.json"))
	require.NoError(t, err)
	var audit map[string]any
	_ = json.Unmarshal(auditBytes, &audit)
	findings, _ := audit["findings"].([]any)
	assert.Len(t, findings, 0, "fabricated citation must be dropped by post-validator")
	// And the recomputed verdict should drop to safe since no findings survived.
	assert.Equal(t, "safe", audit["verdict"])
}

// TestEventsAppendedToDisk asserts emit_progress writes one line per event
// to events.jsonl in the correct shape for the SSE bridge to consume.
func TestEventsAppendedToDisk(t *testing.T) {
	target := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(target, "x"), []byte("y"), 0o600))

	c, _ := newStatefulTestClient(t)
	ctx := context.Background()

	startResp, _ := c.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "assay_scan_start",
			Arguments: map[string]any{"target": target},
		},
	})
	var sb struct {
		ScanID  string `json:"scan_id"`
		ScanDir string `json:"scan_dir"`
	}
	_ = json.Unmarshal([]byte(textOf(t, startResp)), &sb)

	stages := []string{"triage", "claims", "threat_model"}
	for _, st := range stages {
		_, _ = c.CallTool(ctx, mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "assay_emit_progress",
				Arguments: map[string]any{
					"scan_id": sb.ScanID,
					"stage":   st,
					"status":  "complete",
				},
			},
		})
	}

	data, err := os.ReadFile(filepath.Join(sb.ScanDir, "events.jsonl"))
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	// 1 synthetic start event from scan_start + 3 from emit_progress.
	assert.GreaterOrEqual(t, len(lines), 4)
	last := map[string]any{}
	_ = json.Unmarshal([]byte(lines[len(lines)-1]), &last)
	assert.Equal(t, "threat_model", last["stage"])
	assert.Equal(t, "complete", last["status"])
}

// TestRecordFindingRejectsMissingFields enforces the minimum schema before
// findings are persisted.
func TestRecordFindingRejectsMissingFields(t *testing.T) {
	target := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(target, "x"), []byte("y"), 0o600))
	c, _ := newStatefulTestClient(t)
	ctx := context.Background()
	startResp, _ := c.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "assay_scan_start",
			Arguments: map[string]any{"target": target},
		},
	})
	var sb struct {
		ScanID string `json:"scan_id"`
	}
	_ = json.Unmarshal([]byte(textOf(t, startResp)), &sb)

	res, err := c.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "assay_record_finding",
			Arguments: map[string]any{
				"scan_id": sb.ScanID,
				"finding": map[string]any{"id": "missing-fields"}, // no severity/category/title
			},
		},
	})
	require.NoError(t, err)
	assert.True(t, res.IsError)
	assert.Contains(t, textOf(t, res), "severity")
}
