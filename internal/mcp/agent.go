package mcp

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Agent encapsulates launching one MCP-capable CLI to drive the assay MCP
// server. The assay MCP server (its assay_* tools + the methodology prompt) is
// identical for every agent — only the launch mechanics differ: the binary,
// how the MCP server is wired in, the headless flags, the output-stream shape,
// and which env var carries the API key. Adding a new agent (gemini, codex, …)
// is one Agent implementation; nothing about the scan itself changes.
type Agent interface {
	// ID is the agent id, matching provider.AgentID (e.g. "claude-code").
	ID() string
	// Binary is the CLI binary name/path that gets exec'd.
	Binary() string
	// Available returns nil if the CLI is installed and usable.
	Available() error
	// KeyEnvVar is the env var the CLI reads its API key from (e.g.
	// "CURSOR_API_KEY"), or "" if the agent authenticates another way
	// (e.g. Claude Code's subscription bearer).
	KeyEnvVar() string
	// BuildLaunch prepares one scan launch: it writes any per-agent MCP config,
	// returns the CLI args + extra env + a cleanup func. The assay MCP server is
	// delivered to the CLI in whatever way that CLI expects.
	BuildLaunch(p LaunchParams) (Launch, error)
	// ParseStream consumes the CLI's stdout, surfacing events and the session id.
	ParseStream(r io.Reader, onEvent func(StreamEvent)) (sessionID string)
}

// LaunchParams are the scan-launch inputs common to every agent. Each agent
// maps the subset it supports onto its own CLI flags.
type LaunchParams struct {
	AssayBin        string // assay binary to run as the MCP server
	Prompt          string // the methodology-driving prompt
	Offline         bool
	Model           string
	MaxTurns        int
	Subagents       bool
	AllowedTools    string
	ResumeSessionID string
	// APIKey, when non-empty, is injected as the agent's KeyEnvVar so the
	// spawned CLI authenticates with the user's frontend-supplied key.
	APIKey string
}

// Launch is the result of BuildLaunch: how to exec the agent for one scan.
type Launch struct {
	Args    []string
	Env     []string // extra env appended to os.Environ() (e.g. key injection)
	Dir     string   // working directory (empty = inherit); agents whose CLI
	// reads MCP config from the cwd (gemini, codex) set a temp workspace here.
	Cleanup func() // remove temp MCP config / workspace; never nil
}

// AgentFor returns the Agent implementation for an agent id. bin overrides the
// default binary name (used by tests / --claude-bin). Unknown ids and the
// direct-API ids return an error — the latter are not MCP-spawn agents.
func AgentFor(id, bin string) (Agent, error) {
	switch id {
	case "", "claude-code":
		return ClaudeAgent{bin: orDefault(bin, "claude")}, nil
	case "cursor-agent", "cursor":
		return CursorAgent{bin: orDefault(bin, "cursor-agent")}, nil
	case "gemini-cli", "gemini":
		return GeminiAgent{bin: orDefault(bin, "gemini")}, nil
	case "codex-cli", "codex":
		return CodexAgent{bin: orDefault(bin, "codex")}, nil
	default:
		return nil, fmt.Errorf("agent %q is not an MCP-spawn agent (no CLI adapter)", id)
	}
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// --- Claude Code ---

// ClaudeAgent drives `claude -p` (the existing, default path). Its launch is the
// exact behavior shipped before the agent abstraction — buildClaudeArgs +
// writeMCPConfig + parseStreamJSON — so the Claude path is byte-identical.
type ClaudeAgent struct{ bin string }

func (a ClaudeAgent) ID() string        { return "claude-code" }
func (a ClaudeAgent) Binary() string    { return a.bin }
func (a ClaudeAgent) Available() error  { return CheckClaudeAvailable(a.bin) }
func (a ClaudeAgent) KeyEnvVar() string { return "ANTHROPIC_API_KEY" }

func (a ClaudeAgent) BuildLaunch(p LaunchParams) (Launch, error) {
	mcpCfg, err := writeMCPConfig(p.AssayBin, p.Offline)
	if err != nil {
		return Launch{}, fmt.Errorf("write mcp config: %w", err)
	}
	cleanup := func() { _ = os.Remove(mcpCfg) }
	// --bare is safe only with an API key (it skips keychain reads). True when a
	// key is injected here or already present in the environment.
	apiKeyAuth := p.APIKey != "" || fromEnvHasKey()
	cfg := SpawnConfig{
		ClaudeBin:       a.bin,
		AllowedTools:    p.AllowedTools,
		Offline:         p.Offline,
		Model:           p.Model,
		MaxTurns:        p.MaxTurns,
		Subagents:       p.Subagents,
		ResumeSessionID: p.ResumeSessionID,
		APIKeyAuth:      apiKeyAuth,
	}
	args := buildClaudeArgs(cfg, mcpCfg, p.Prompt, claudeCapabilities(a.bin))
	var env []string
	if p.APIKey != "" {
		env = append(env, "ANTHROPIC_API_KEY="+p.APIKey)
	}
	return Launch{Args: args, Env: env, Cleanup: cleanup}, nil
}

func (a ClaudeAgent) ParseStream(r io.Reader, onEvent func(StreamEvent)) string {
	return parseStreamJSON(r, onEvent)
}

// --- Cursor (cursor-agent) ---

// CursorAgent drives Cursor's headless CLI (`cursor-agent -p`). Cursor loads MCP
// servers from <workspace>/.cursor/mcp.json, so each launch writes a temp
// workspace with the assay server configured and points --workspace at it.
// --plan keeps Cursor read-only (no edits/shell); --approve-mcps + --force run
// headless without approval prompts. Cursor reads its key from CURSOR_API_KEY.
type CursorAgent struct{ bin string }

func (a CursorAgent) ID() string        { return "cursor-agent" }
func (a CursorAgent) Binary() string    { return a.bin }
func (a CursorAgent) KeyEnvVar() string { return "CURSOR_API_KEY" }

func (a CursorAgent) Available() error {
	if _, err := exec.LookPath(a.bin); err != nil {
		return fmt.Errorf("%s not on PATH: install Cursor's CLI (cursor-agent)", a.bin)
	}
	return nil
}

func (a CursorAgent) BuildLaunch(p LaunchParams) (Launch, error) {
	ws, err := os.MkdirTemp("", "assay-cursor-ws-*")
	if err != nil {
		return Launch{}, fmt.Errorf("cursor workspace: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(ws) }
	cursorDir := filepath.Join(ws, ".cursor")
	if err := os.MkdirAll(cursorDir, 0o750); err != nil {
		cleanup()
		return Launch{}, fmt.Errorf("cursor .cursor dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(cursorDir, "mcp.json"), mcpServersJSON(p.AssayBin, p.Offline), 0o600); err != nil {
		cleanup()
		return Launch{}, fmt.Errorf("cursor mcp.json: %w", err)
	}

	maxTurns := p.MaxTurns
	if maxTurns <= 0 {
		maxTurns = defaultMaxTurns
	}
	args := []string{
		"-p", p.Prompt,
		"--workspace", ws,
		"--output-format", "stream-json",
		"--plan",         // read-only: analyze, no edits/shell (Assay is a read-only scanner)
		"--approve-mcps", // auto-approve the assay MCP server (headless, no prompt)
		"--force",        // run without per-tool approval prompts (bounded by --plan)
		"--trust",        // trust the temp workspace (headless)
		"--max-turns", strconv.Itoa(maxTurns),
	}
	if p.Model != "" {
		args = append(args, "--model", p.Model)
	}
	var env []string
	if p.APIKey != "" {
		env = append(env, "CURSOR_API_KEY="+p.APIKey)
	}
	return Launch{Args: args, Env: env, Cleanup: cleanup}, nil
}

func (a CursorAgent) ParseStream(r io.Reader, onEvent func(StreamEvent)) string {
	// cursor-agent's stream-json emits the same family of envelopes as Claude
	// (type/session_id/usage/tool_use); the tolerant parser skips fields it
	// doesn't recognize, so it works for both. Refined against live output.
	return parseStreamJSON(r, onEvent)
}

// --- Gemini CLI ---

// GeminiAgent drives Google's Gemini CLI (`gemini -p`). Gemini loads MCP servers
// from <cwd>/.gemini/settings.json, so each launch runs in a temp workspace with
// that file written; --yolo auto-approves tool calls for headless use. Key:
// GEMINI_API_KEY.
//
// NOTE: gemini is not installed on the dev machine where this was written — the
// flag set follows Gemini CLI's documented interface and should be confirmed
// against a live install. The scan itself is robust: progress + the audit flow
// through the assay MCP server's events.jsonl, not this CLI's stdout.
type GeminiAgent struct{ bin string }

func (a GeminiAgent) ID() string        { return "gemini-cli" }
func (a GeminiAgent) Binary() string    { return a.bin }
func (a GeminiAgent) KeyEnvVar() string { return "GEMINI_API_KEY" }

func (a GeminiAgent) Available() error {
	if _, err := exec.LookPath(a.bin); err != nil {
		return fmt.Errorf("%s not on PATH: install the Gemini CLI", a.bin)
	}
	return nil
}

func (a GeminiAgent) BuildLaunch(p LaunchParams) (Launch, error) {
	ws, err := os.MkdirTemp("", "assay-gemini-ws-*")
	if err != nil {
		return Launch{}, fmt.Errorf("gemini workspace: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(ws) }
	gdir := filepath.Join(ws, ".gemini")
	if err := os.MkdirAll(gdir, 0o750); err != nil {
		cleanup()
		return Launch{}, fmt.Errorf("gemini .gemini dir: %w", err)
	}
	// Gemini reads MCP servers from .gemini/settings.json ({"mcpServers": …}) —
	// the same shape as everything else.
	if err := os.WriteFile(filepath.Join(gdir, "settings.json"), mcpServersJSON(p.AssayBin, p.Offline), 0o600); err != nil {
		cleanup()
		return Launch{}, fmt.Errorf("gemini settings.json: %w", err)
	}
	args := []string{"-p", p.Prompt, "--yolo"}
	if p.Model != "" {
		args = append(args, "--model", p.Model)
	}
	var env []string
	if p.APIKey != "" {
		env = append(env, "GEMINI_API_KEY="+p.APIKey)
	}
	return Launch{Args: args, Env: env, Dir: ws, Cleanup: cleanup}, nil
}

func (a GeminiAgent) ParseStream(r io.Reader, onEvent func(StreamEvent)) string {
	return parseStreamJSON(r, onEvent) // best-effort; progress comes via events.jsonl
}

// --- Codex CLI ---

// CodexAgent drives OpenAI's Codex CLI (`codex exec`). Codex reads MCP servers
// from $CODEX_HOME/config.toml, so each launch points CODEX_HOME at a temp dir
// with that config; --full-auto runs headless without approval prompts. Key:
// OPENAI_API_KEY.
//
// NOTE: codex is not installed on the dev machine — the flag set follows Codex
// CLI's documented interface and should be confirmed against a live install.
type CodexAgent struct{ bin string }

func (a CodexAgent) ID() string        { return "codex-cli" }
func (a CodexAgent) Binary() string    { return a.bin }
func (a CodexAgent) KeyEnvVar() string { return "OPENAI_API_KEY" }

func (a CodexAgent) Available() error {
	if _, err := exec.LookPath(a.bin); err != nil {
		return fmt.Errorf("%s not on PATH: install the Codex CLI", a.bin)
	}
	return nil
}

func (a CodexAgent) BuildLaunch(p LaunchParams) (Launch, error) {
	home, err := os.MkdirTemp("", "assay-codex-home-*")
	if err != nil {
		return Launch{}, fmt.Errorf("codex home: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(home) }
	if err := os.WriteFile(filepath.Join(home, "config.toml"), codexConfigTOML(p.AssayBin, p.Offline), 0o600); err != nil {
		cleanup()
		return Launch{}, fmt.Errorf("codex config.toml: %w", err)
	}
	args := []string{"exec", "--full-auto"}
	if p.Model != "" {
		args = append(args, "--model", p.Model)
	}
	args = append(args, p.Prompt)
	env := []string{"CODEX_HOME=" + home}
	if p.APIKey != "" {
		env = append(env, "OPENAI_API_KEY="+p.APIKey)
	}
	return Launch{Args: args, Env: env, Cleanup: cleanup}, nil
}

func (a CodexAgent) ParseStream(r io.Reader, onEvent func(StreamEvent)) string {
	return parseStreamJSON(r, onEvent) // best-effort; progress comes via events.jsonl
}

// codexConfigTOML renders the Codex MCP-server config (TOML) for the assay
// stdio server — the same server, expressed in Codex's config format.
func codexConfigTOML(assayBin string, offline bool) []byte {
	args := []string{"mcp", "--transport", "stdio"}
	if offline {
		args = append(args, "--offline")
	}
	var b strings.Builder
	b.WriteString("[mcp_servers.assay]\n")
	fmt.Fprintf(&b, "command = %q\n", assayBin)
	b.WriteString("args = [")
	for i, a := range args {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%q", a)
	}
	b.WriteString("]\n")
	return []byte(b.String())
}

// fromEnvHasKey reports whether an Anthropic API key is present in the
// environment (gates Claude's --bare, which authenticates strictly via the key).
func fromEnvHasKey() bool { return os.Getenv("ANTHROPIC_API_KEY") != "" }
