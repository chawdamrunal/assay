package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/chawdamrunal/assay/internal/github"
	assaymcp "github.com/chawdamrunal/assay/internal/mcp"
	"github.com/chawdamrunal/assay/internal/provider"
	"github.com/chawdamrunal/assay/internal/store"
)

// StatusCheck is one row in the /api/status response. Settings renders these
// as labeled "Connections" rows with a semantic dot (ok / warn / error).
type StatusCheck struct {
	// Name is the human-facing label ("Claude Code CLI", "Assay MCP server").
	Name string `json:"name"`
	// Kind classifies the check for the FE icon: "claude-code", "mcp",
	// "auth", "filesystem", "hook".
	Kind string `json:"kind"`
	// Level is "ok" | "warn" | "error" — drives the colored dot.
	Level string `json:"level"`
	// Detail is the one-line explanation (path, version, error message).
	Detail string `json:"detail"`
	// LastChecked is when this row was probed (server time, RFC3339).
	LastChecked string `json:"last_checked"`
}

// StatusResponse is the body of GET /api/status.
type StatusResponse struct {
	GeneratedAt string        `json:"generated_at"`
	Checks      []StatusCheck `json:"checks"`
}

// StatusDeps are the dependencies the status probe needs.
type StatusDeps struct {
	ClaudeBin  string // claude CLI binary (typically "claude" on PATH)
	AssayBin   string // path to the running assay binary (for MCP self-check)
	ScansDir   string // ~/.assay/scans — verified writable
	HookScript string // ~/.assay/hooks/assay-pre-install.sh — present + executable when installed
	// KeychainService is the OS keychain service name (production: "assay"); used
	// to report which direct-API providers have a key configured.
	KeychainService string
}

// NewStatusHandler returns the http.Handler for GET /api/status. Each call
// re-probes every check (cheap; bounded under 2 seconds total).
func NewStatusHandler(deps StatusDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			WriteJSONError(w, http.StatusMethodNotAllowed, "status requires GET")
			return
		}
		now := time.Now().UTC().Format(time.RFC3339)
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		checks := []StatusCheck{
			probeClaudeCode(ctx, deps.ClaudeBin, now),
			probeAssayMCP(ctx, deps.AssayBin, now),
			probeAuth(ctx, now),
			probeGitHub(deps.KeychainService, now),
			probeFilesystem(deps.ScansDir, now),
			probeHook(deps.HookScript, now),
		}
		checks = append(checks, probeProviderKeys(deps.KeychainService, now)...)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(StatusResponse{
			GeneratedAt: now,
			Checks:      checks,
		})
	})
}

// probeGitHub reports whether repo fetching works and whether private repos
// are reachable. git must be on PATH; a resolvable token (keychain → env → gh
// CLI) enables private clones. The token value is never exposed — only its
// source label ("keychain", "env:GITHUB_TOKEN", "gh-cli").
func probeGitHub(keychainService, now string) StatusCheck {
	c := StatusCheck{Name: "GitHub fetch", Kind: "github", LastChecked: now}
	if _, err := exec.LookPath("git"); err != nil {
		c.Level = "warn"
		c.Detail = "git not on PATH — GitHub repo fetch disabled"
		return c
	}
	var kcToken string
	if keychainService != "" {
		if t, err := store.NewKeyring(keychainService).GetGitHubToken(); err == nil {
			kcToken = t
		}
	}
	// Own tight budget so a slow `gh auth token` never blows the status
	// endpoint's overall deadline, regardless of how much the shared ctx has left.
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	c.Level = "ok"
	if _, source := github.ResolveTokenContext(ctx, kcToken); source == "none" {
		c.Detail = "public repos only — add a GitHub token to scan private repos"
	} else {
		c.Detail = "private repos enabled — token via " + source
	}
	return c
}

// probeClaudeCode runs `claude --version` with a tight timeout. We don't
// require claude to be present — when absent, level=warn (only MCP mode
// needs it; legacy + fake modes work without).
func probeClaudeCode(ctx context.Context, claudeBin, now string) StatusCheck {
	c := StatusCheck{Name: "Claude Code CLI", Kind: "claude-code", LastChecked: now}
	if claudeBin == "" {
		claudeBin = "claude"
	}
	path, err := exec.LookPath(claudeBin)
	if err != nil {
		c.Level = "warn"
		c.Detail = fmt.Sprintf("%s not on PATH — required for the default MCP scan mode. Install: https://claude.com/code", claudeBin)
		return c
	}
	out, err := runWithCtx(ctx, claudeBin, "--version")
	if err != nil {
		c.Level = "error"
		c.Detail = fmt.Sprintf("%s --version failed: %v", claudeBin, err)
		return c
	}
	c.Level = "ok"
	c.Detail = strings.TrimSpace(out) + " — at " + path
	return c
}

// probeAssayMCP does an in-process JSON-RPC handshake with our own MCP server
// via `assay mcp --transport stdio`. Verifies the binary boots and serves
// the tools/list method — proving the MCP wiring is intact end-to-end.
func probeAssayMCP(ctx context.Context, assayBin, now string) StatusCheck {
	c := StatusCheck{Name: "Assay MCP server", Kind: "mcp", LastChecked: now}
	if assayBin == "" {
		exe, err := os.Executable()
		if err != nil {
			c.Level = "error"
			c.Detail = "could not locate assay binary: " + err.Error()
			return c
		}
		assayBin = exe
	}
	probeCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, assayBin, "mcp", "--transport", "stdio") // #nosec G204 -- assayBin is our own executable
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		c.Level = "error"
		c.Detail = "could not start assay mcp: " + err.Error()
		return c
	}
	go func() {
		_, _ = stdin.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"status-probe","version":"0"}}}` + "\n"))
		_, _ = stdin.Write([]byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n"))
		_, _ = stdin.Write([]byte(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n"))
		_ = stdin.Close()
	}()
	buf := make([]byte, 16384)
	n, _ := stdout.Read(buf)
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	body := string(buf[:n])
	if strings.Contains(body, `"serverInfo"`) && strings.Contains(body, `"assay"`) {
		c.Level = "ok"
		c.Detail = "stdio handshake succeeded; " + assaymcp.Version + " — " + assayBin
		return c
	}
	c.Level = "error"
	c.Detail = "MCP server did not respond to initialize. First bytes: " + truncate(body, 120)
	return c
}

// probeAuth shells out to `assay auth status` and parses the active method.
// We can't import internal/auth here without a circular dep risk; the CLI
// is the canonical source of truth anyway. Tight 1s timeout.
func probeAuth(parent context.Context, now string) StatusCheck {
	c := StatusCheck{Name: "Anthropic credentials", Kind: "auth", LastChecked: now}
	exe, err := os.Executable()
	if err != nil {
		exe = "assay"
	}
	ctx, cancel := context.WithTimeout(parent, 1*time.Second)
	defer cancel()
	out, err := runWithCtx(ctx, exe, "auth", "status")
	if err != nil {
		c.Level = "warn"
		c.Detail = "assay auth status failed: " + truncate(err.Error(), 80) +
			" — legacy scan mode needs API key, MCP mode does not"
		return c
	}
	if strings.Contains(out, "Active: claude-code") {
		c.Level = "ok"
		c.Detail = "Claude Code OAuth bearer (subscription) — covers MCP + legacy modes"
		return c
	}
	if strings.Contains(out, "Active: env") || strings.Contains(out, "Active: assay-key") {
		c.Level = "ok"
		// Try to pull the friendly source line.
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "Active:") {
				c.Detail = line + " — direct API key configured"
				return c
			}
		}
		c.Detail = "API key configured"
		return c
	}
	c.Level = "warn"
	c.Detail = "no auth method active — only fake-mode + MCP-mode-via-claude-code work without"
	return c
}

// probeProviderKeys reports one row per direct-API provider indicating whether
// an API key is configured (in the OS keychain). Booleans/labels only — never
// the key value. The Kind is "<vendor>-key" so the FE can pick a provider icon.
func probeProviderKeys(keychainService, now string) []StatusCheck {
	out := make([]StatusCheck, 0, len(provider.APIProviders()))
	var kr *store.Keyring
	if keychainService != "" {
		kr = store.NewKeyring(keychainService)
	}
	for _, id := range provider.APIProviders() {
		c := StatusCheck{Name: providerKeyName(id), Kind: id.Vendor() + "-key", LastChecked: now}
		if kr != nil && kr.HasProviderKey(string(id)) {
			c.Level = "ok"
			c.Detail = "API key configured"
		} else {
			c.Level = "warn"
			c.Detail = "no key set — add one in Settings to scan with " + string(id)
		}
		out = append(out, c)
	}
	return out
}

func providerKeyName(id provider.AgentID) string {
	switch id {
	case provider.AgentAnthropicAPI:
		return "Anthropic API key"
	case provider.AgentGeminiAPI:
		return "Gemini API key"
	case provider.AgentOpenAIAPI:
		return "OpenAI API key"
	default:
		return string(id) + " key"
	}
}

func probeFilesystem(scansDir, now string) StatusCheck {
	c := StatusCheck{Name: "Assay data directory", Kind: "filesystem", LastChecked: now}
	if scansDir == "" {
		c.Level = "warn"
		c.Detail = "scans dir not configured"
		return c
	}
	if err := os.MkdirAll(scansDir, 0o750); err != nil {
		c.Level = "error"
		c.Detail = "mkdir failed: " + err.Error()
		return c
	}
	// Verify writeable by touching a probe file.
	probe := filepath.Join(scansDir, ".status-probe")
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil { // #nosec G306 -- probe file
		c.Level = "error"
		c.Detail = "scans dir not writable: " + err.Error()
		return c
	}
	_ = os.Remove(probe)
	// Count completed + failed + pending for context.
	complete, failed, pending := countScansByStatus(scansDir)
	c.Level = "ok"
	c.Detail = fmt.Sprintf("%s — %d complete · %d failed · %d pending", scansDir, complete, failed, pending)
	return c
}

func probeHook(scriptPath, now string) StatusCheck {
	c := StatusCheck{Name: "Pre-install gate hook", Kind: "hook", LastChecked: now}
	if scriptPath == "" {
		// Default location used by `assay hook install`.
		home, _ := os.UserHomeDir()
		scriptPath = filepath.Join(home, ".assay", "hooks", "assay-pre-install.sh")
	}
	info, err := os.Stat(scriptPath)
	if err != nil {
		c.Level = "warn"
		c.Detail = "not installed — run `assay hook install` to gate /plugin install commands"
		return c
	}
	mode := info.Mode().Perm()
	if mode&0o100 == 0 {
		c.Level = "warn"
		c.Detail = scriptPath + " — exists but not executable; chmod +x"
		return c
	}
	c.Level = "ok"
	c.Detail = scriptPath
	return c
}

// runWithCtx is a small exec helper that returns stdout (combined with
// stderr) and respects a context deadline.
func runWithCtx(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- caller-bounded
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// countScansByStatus walks the scans directory and counts per-status without
// allocating per-entry structures. Mirrors the logic in handleListScans but
// returns scalars.
func countScansByStatus(scansDir string) (complete, failed, pending int) {
	entries, err := os.ReadDir(scansDir)
	if err != nil {
		return 0, 0, 0
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		targetDir := filepath.Join(scansDir, e.Name())
		scanDirs, _ := os.ReadDir(targetDir)
		for _, sd := range scanDirs {
			if !sd.IsDir() {
				continue
			}
			scanDir := filepath.Join(targetDir, sd.Name())
			switch scanDirStatus(scanDir) {
			case "complete":
				complete++
			case "failed":
				failed++
			default:
				pending++
			}
		}
	}
	return
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
