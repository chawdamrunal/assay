package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestClient brings up the real assay MCP server in-process and returns a
// connected client. The server is the same one production uses; only the
// transport is different (in-memory instead of stdio).
func newTestClient(t *testing.T) *mcpclient.Client {
	t.Helper()
	s := NewServer()
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

func TestMCPServerListsTools(t *testing.T) {
	c := newTestClient(t)
	resp, err := c.ListTools(context.Background(), mcp.ListToolsRequest{})
	require.NoError(t, err)
	names := []string{}
	for _, t := range resp.Tools {
		names = append(names, t.Name)
	}
	assert.Contains(t, names, "assay_list_files")
	assert.Contains(t, names, "assay_read_file")
}

func TestMCPListFiles(t *testing.T) {
	target := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(target, "a.txt"), []byte("hi"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(target, "sub"), 0o750))

	c := newTestClient(t)
	res, err := c.CallTool(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "assay_list_files",
			Arguments: map[string]any{"target": target},
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError, "tool returned error: %+v", res.Content)
	text := textOf(t, res)
	assert.Contains(t, text, "a.txt")
	assert.Contains(t, text, "sub/")
}

func TestMCPReadFileWithLineRange(t *testing.T) {
	target := t.TempDir()
	content := strings.Join([]string{"one", "two", "three", "four", "five"}, "\n")
	require.NoError(t, os.WriteFile(filepath.Join(target, "f.txt"), []byte(content), 0o600))

	c := newTestClient(t)
	res, err := c.CallTool(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "assay_read_file",
			Arguments: map[string]any{
				"target":     target,
				"path":       "f.txt",
				"start_line": float64(2),
				"end_line":   float64(4),
			},
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	text := textOf(t, res)
	// Output is prefixed `path:line: content` per FS.ReadFile contract.
	assert.Contains(t, text, "f.txt:2: two")
	assert.Contains(t, text, "f.txt:4: four")
	assert.NotContains(t, text, "f.txt:1: one")
	assert.NotContains(t, text, "f.txt:5: five")
}

func TestMCPRejectsPathEscape(t *testing.T) {
	target := t.TempDir()
	c := newTestClient(t)
	res, err := c.CallTool(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "assay_read_file",
			Arguments: map[string]any{
				"target": target,
				"path":   "../../../../etc/passwd",
			},
		},
	})
	require.NoError(t, err) // protocol succeeded; tool reported isError
	assert.True(t, res.IsError, "path-escape attempt must be rejected by the tool")
	assert.Contains(t, textOf(t, res), "escapes root")
}

func TestMethodologyPromptListedAndSubstitutes(t *testing.T) {
	c := newTestClient(t)
	listed, err := c.ListPrompts(context.Background(), mcp.ListPromptsRequest{})
	require.NoError(t, err)
	var found bool
	for _, p := range listed.Prompts {
		if p.Name == "assay_methodology" {
			found = true
			break
		}
	}
	require.True(t, found, "assay_methodology prompt should be registered")

	got, err := c.GetPrompt(context.Background(), mcp.GetPromptRequest{
		Params: mcp.GetPromptParams{
			Name:      "assay_methodology",
			Arguments: map[string]string{"target": "/abs/path/sample"},
		},
	})
	require.NoError(t, err)
	require.Len(t, got.Messages, 1)
	textContent, ok := got.Messages[0].Content.(mcp.TextContent)
	require.True(t, ok)
	body := textContent.Text
	assert.Contains(t, body, "/abs/path/sample", "target must be substituted into prompt body")
	assert.Contains(t, body, "assay_scan_start", "prompt must mention the start tool")
	assert.Contains(t, body, "assay_finalize_scan", "prompt must mention the finalize tool")
	assert.NotContains(t, body, "{{TARGET}}", "placeholder must be fully replaced")
}

func TestMethodologyPromptRequiresTarget(t *testing.T) {
	c := newTestClient(t)
	_, err := c.GetPrompt(context.Background(), mcp.GetPromptRequest{
		Params: mcp.GetPromptParams{Name: "assay_methodology"},
	})
	require.Error(t, err)
}

func TestMCPRejectsRelativeTarget(t *testing.T) {
	c := newTestClient(t)
	res, err := c.CallTool(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "assay_list_files",
			Arguments: map[string]any{"target": "relative/path"},
		},
	})
	require.NoError(t, err)
	assert.True(t, res.IsError)
	assert.Contains(t, textOf(t, res), "absolute")
}

// textOf collects all TextContent fragments from a CallToolResult.
func textOf(t *testing.T, r *mcp.CallToolResult) string {
	t.Helper()
	var b strings.Builder
	for _, c := range r.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}
