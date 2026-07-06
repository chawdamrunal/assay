package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// readOnlyDenylistBase is passed to `claude --disallowedTools` so the scan
// agent can never execute code, write files, or reach the network through
// Claude Code's built-in tools — only the read-only assay_* MCP tools. This is
// a hard block independent of permission mode: even if the host's settings put
// the session in bypassPermissions (as observed with --bare), a disallowed
// tool is never offered to the model. The block is session-wide, so subagents
// inherit it and stay read-only too. Assay is a read-only scanner; this
// enforces it.
//
// In single-agent mode the Task tool is ALSO blocked (appended below) so a
// normal scan can't spawn subagents. Deep mode (Subagents) lifts the Task
// block and adds Task to the allowlist instead.
const readOnlyDenylistBase = "Bash,Edit,Write,NotebookEdit,WebFetch,WebSearch"

// defaultMaxTurns bounds how many agent turns the Claude Code subprocess may
// take before it aborts. The 7-stage methodology issues many tool calls, so
// the floor is generous; the cap exists only to stop a runaway/looping scan
// from burning the user's subscription quota indefinitely.
const defaultMaxTurns = 50

// defaultScanTimeout is the wall-clock ceiling for a single MCP-mode scan.
// Without it a stalled subprocess (model hang, wedged MCP call) runs forever
// because the serve path passes a context with no deadline.
const defaultScanTimeout = 15 * time.Minute

// SpawnConfig controls how a Claude Code subprocess is launched to drive an
// MCP-mode scan. Defaults are filled in by NewSpawner; callers usually only
// override AssayBin when running tests against a non-PATH binary.
type SpawnConfig struct {
	// ClaudeBin is the Claude Code CLI to invoke. Default: "claude".
	ClaudeBin string
	// AssayBin is the assay binary that will be launched as the MCP server.
	// Default: "assay" (resolved on PATH). Tests pass an absolute path.
	AssayBin string
	// AllowedTools restricts which MCP tools Claude is allowed to call
	// non-interactively. Default: "mcp__assay__*" — every assay_* tool.
	AllowedTools string
	// ExtraArgs are appended to the claude command line. Useful for tests.
	ExtraArgs []string
	// Stderr receives the subprocess's stderr for surfacing errors.
	// Default: io.Discard.
	Stderr io.Writer
	// Offline, when true, instructs the methodology prompt to skip outbound
	// network calls (notably assay_osv_lookup). Threaded through from the
	// scan request so the API/CLI `offline` flag is no longer silently
	// dropped in MCP mode.
	Offline bool
	// Model is the Anthropic model ID passed to `claude -p --model`. When
	// non-empty the subprocess is strictly pinned to this model — the
	// user's Settings choice (e.g. claude-sonnet-4-6) is honored rather
	// than letting Claude Code pick its default. Leave empty to inherit
	// Claude Code's own default (the subscription-appropriate model).
	Model string
	// MaxTurns caps the agent turns the subprocess may take (passed as
	// --max-turns). Zero → defaultMaxTurns. Guards against runaway loops.
	MaxTurns int
	// ScanTimeout is the wall-clock ceiling for the whole scan. Zero →
	// defaultScanTimeout. The subprocess is SIGKILLed when it elapses.
	ScanTimeout time.Duration
	// ResumeSessionID, when non-empty, resumes a prior Claude Code session via
	// `--resume`. Used by diff-mode so a re-scan reuses the previous scan's
	// context (threat model, prior reads) instead of starting cold — large
	// token savings on the user's quota. Capture it from a prior run via
	// SessionIDOut.
	ResumeSessionID string
	// OnStreamEvent, when set, receives each parsed envelope from the
	// subprocess's stream-json stdout (token usage, cost, tool-call names,
	// the terminal result). Lets the caller surface live cost/progress and
	// detect a stalled agent. nil → events are discarded.
	OnStreamEvent func(StreamEvent)
	// SessionIDOut, when non-nil, is set to the Claude Code session_id parsed
	// from the stream once the subprocess finishes. Persist it to enable
	// --resume on a later diff-mode scan of the same target.
	SessionIDOut *string
	// Subagents enables deep mode: the agent dispatches each Stage-5 threat
	// investigation as a parallel Task subagent (own context window) instead
	// of investigating sequentially. Permits the Task tool and injects the
	// deep-mode instruction into the prompt. Opt-in (more quota). Subagents
	// still inherit the read-only --disallowedTools block.
	Subagents bool
	// APIKeyAuth reports whether the subprocess will authenticate via an
	// Anthropic API key (ANTHROPIC_API_KEY) rather than the Claude Code OAuth
	// subscription bearer. It gates --bare: in --bare mode Claude Code skips
	// keychain reads and authenticates STRICTLY via the API key, so passing
	// --bare on the (default) subscription path makes every scan fail instantly
	// with "Not logged in". SpawnScan sets this from auth.FromEnv(); callers
	// don't populate it (it is overwritten before buildClaudeArgs runs).
	APIKeyAuth bool
	// Agent selects which MCP-capable CLI drives the scan. nil → ClaudeAgent
	// (the default), preserving the pre-multi-agent behavior for every existing
	// caller. The assay MCP server is identical for all agents.
	Agent Agent
	// APIKey, when set, is injected as the chosen agent's key env var (e.g.
	// ANTHROPIC_API_KEY / CURSOR_API_KEY) so the spawned CLI authenticates with
	// the user's frontend-supplied key rather than its own login/subscription.
	APIKey string
}

// StreamEvent is one parsed envelope from `claude -p --output-format
// stream-json`. Only the fields Assay acts on are surfaced.
type StreamEvent struct {
	Type         string   // "system" | "assistant" | "user" | "result"
	Subtype      string   // e.g. "init", "success"
	SessionID    string   // present on most envelopes
	CostUSD      float64  // cumulative, present on the terminal "result"
	InputTokens  int      // per-message (assistant) or cumulative (result)
	OutputTokens int      //
	ToolNames    []string // names of tool_use blocks in an assistant turn
	IsError      bool     // set on a failed "result"
}

// CheckClaudeAvailable returns nil if `claude --version` runs cleanly. Use at
// serve boot to decide whether MCP mode is viable for this machine.
func CheckClaudeAvailable(claudeBin string) error {
	if claudeBin == "" {
		claudeBin = "claude"
	}
	if _, err := exec.LookPath(claudeBin); err != nil {
		return fmt.Errorf("%s not on PATH: install via https://claude.com/code", claudeBin)
	}
	out, err := exec.Command(claudeBin, "--version").CombinedOutput() // #nosec G204 -- looked up via PATH
	if err != nil {
		return fmt.Errorf("%s --version failed: %w (output: %s)", claudeBin, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// claudeCaps records which optional `claude` flags the installed CLI supports,
// so Assay can use newer flags where available and degrade gracefully on older
// versions (an unknown flag would make every scan fail).
type claudeCaps struct {
	Bare            bool // --bare (isolate from host CLAUDE.md/hooks/skills/plugins)
	DisallowedTools bool // --disallowedTools (read-only enforcement)
	StrictMCP       bool // --strict-mcp-config (use ONLY our --mcp-config, not the host's)
}

var (
	capsMu    sync.Mutex
	capsCache = map[string]claudeCaps{}
)

// claudeCapabilities probes `claude --help` once per binary (cached) to detect
// optional flag support. A probe failure yields the zero value (no optional
// flags), which keeps scans working on the most conservative flag set.
func claudeCapabilities(claudeBin string) claudeCaps {
	if claudeBin == "" {
		claudeBin = "claude"
	}
	capsMu.Lock()
	defer capsMu.Unlock()
	if c, ok := capsCache[claudeBin]; ok {
		return c
	}
	c := claudeCaps{}
	if out, err := exec.Command(claudeBin, "--help").CombinedOutput(); err == nil { // #nosec G204 -- claudeBin validated on PATH
		h := string(out)
		c.Bare = strings.Contains(h, "--bare")
		c.DisallowedTools = strings.Contains(h, "--disallowedTools") || strings.Contains(h, "--disallowed-tools")
		c.StrictMCP = strings.Contains(h, "--strict-mcp-config")
	}
	capsCache[claudeBin] = c
	return c
}

// parseStreamJSON consumes a `claude -p --output-format stream-json` NDJSON
// stream, invoking onEvent for each parsed envelope and returning the session
// id observed (for --resume on a later scan). It is resilient: non-JSON lines
// (startup banners) are skipped, and a line that fails to parse is ignored
// rather than aborting the stream. Pure (io.Reader in) so it is unit-testable
// without spawning a subprocess.
func parseStreamJSON(r io.Reader, onEvent func(StreamEvent)) (sessionID string) {
	sc := bufio.NewScanner(r)
	// Stream-json lines carry whole assistant messages, which can be large.
	sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var raw struct {
			Type         string  `json:"type"`
			Subtype      string  `json:"subtype"`
			SessionID    string  `json:"session_id"`
			TotalCostUSD float64 `json:"total_cost_usd"`
			IsError      bool    `json:"is_error"`
			Message      *struct {
				Usage *struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
				Content []struct {
					Type string `json:"type"`
					Name string `json:"name"`
				} `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(line, &raw); err != nil {
			continue
		}
		if raw.SessionID != "" {
			sessionID = raw.SessionID
		}
		ev := StreamEvent{
			Type:      raw.Type,
			Subtype:   raw.Subtype,
			SessionID: raw.SessionID,
			CostUSD:   raw.TotalCostUSD,
			IsError:   raw.IsError,
		}
		if raw.Message != nil {
			if raw.Message.Usage != nil {
				ev.InputTokens = raw.Message.Usage.InputTokens
				ev.OutputTokens = raw.Message.Usage.OutputTokens
			}
			for _, c := range raw.Message.Content {
				if c.Type == "tool_use" && c.Name != "" {
					ev.ToolNames = append(ev.ToolNames, c.Name)
				}
			}
		}
		if onEvent != nil {
			onEvent(ev)
		}
	}
	return sessionID
}

// SpawnScan launches a Claude Code subprocess that drives an end-to-end scan
// via the assay MCP server. The subprocess:
//   - Boots a fresh assay MCP stdio server (so it can write to events.jsonl /
//     findings.jsonl on this machine's disk).
//   - Receives the methodology prompt with target+scan_id substituted as its
//     `-p` argument.
//   - Calls assay_* tools to walk the 5 stages and produce audit.json+audit.md.
//
// Returns when the subprocess exits. Errors from the subprocess itself become
// the returned error; the caller should also be reading events.jsonl via
// TailEvents to surface per-stage progress.
func SpawnScan(ctx context.Context, cfg SpawnConfig, target, scanID string) error {
	if cfg.AssayBin == "" {
		cfg.AssayBin = "assay"
	}
	if cfg.AllowedTools == "" {
		cfg.AllowedTools = "mcp__assay__*"
	}
	if cfg.Stderr == nil {
		cfg.Stderr = io.Discard
	}
	// Default to the Claude Code agent when none was injected — preserves the
	// exact pre-multi-agent behavior for every existing caller.
	agent := cfg.Agent
	if agent == nil {
		agent = ClaudeAgent{bin: orDefault(cfg.ClaudeBin, "claude")}
	}

	prompt := buildMCPPrompt(target, scanID, cfg.Offline, cfg.Subagents)
	launch, err := agent.BuildLaunch(LaunchParams{
		AssayBin:        cfg.AssayBin,
		Prompt:          prompt,
		Offline:         cfg.Offline,
		Model:           cfg.Model,
		MaxTurns:        cfg.MaxTurns,
		Subagents:       cfg.Subagents,
		AllowedTools:    cfg.AllowedTools,
		ResumeSessionID: cfg.ResumeSessionID,
		APIKey:          cfg.APIKey,
	})
	if err != nil {
		return err
	}
	defer launch.Cleanup()

	// Bound the scan with a wall-clock deadline. exec.CommandContext SIGKILLs
	// the subprocess when scanCtx is done, so a stalled agent (model hang,
	// wedged MCP call) can no longer run forever — the serve path passes a
	// context with no deadline of its own.
	timeout := cfg.ScanTimeout
	if timeout <= 0 {
		timeout = defaultScanTimeout
	}
	scanCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(scanCtx, agent.Binary(), launch.Args...) // #nosec G204 -- binary validated on PATH; args constructed in-process
	cmd.Stderr = cfg.Stderr
	if launch.Dir != "" {
		cmd.Dir = launch.Dir
	}
	if len(launch.Env) > 0 {
		cmd.Env = append(os.Environ(), launch.Env...)
	}

	// Consume stdout (stream-json) in a goroutine: surface live cost/tool
	// events to the caller and capture the session_id for --resume.
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("%s stdout pipe: %w", agent.ID(), err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawn %s: %w", agent.ID(), err)
	}
	var sessionID string
	streamDone := make(chan struct{})
	go func() {
		sessionID = agent.ParseStream(stdout, cfg.OnStreamEvent)
		close(streamDone)
	}()

	waitErr := cmd.Wait()
	<-streamDone // ensure the parser drained before reading sessionID
	if cfg.SessionIDOut != nil {
		*cfg.SessionIDOut = sessionID
	}
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			return fmt.Errorf("%s subprocess exited %d", agent.ID(), exitErr.ExitCode())
		}
		return fmt.Errorf("spawn %s: %w", agent.ID(), waitErr)
	}
	return nil
}

// buildClaudeArgs assembles the `claude -p` command line for a scan. Split out
// from SpawnScan so the arg construction is unit testable without spawning a
// subprocess. Callers must have already defaulted cfg.AllowedTools; maxTurns is
// defaulted here. caps gates flags the installed claude may not support.
func buildClaudeArgs(cfg SpawnConfig, mcpCfg, prompt string, caps claudeCaps) []string {
	maxTurns := cfg.MaxTurns
	if maxTurns <= 0 {
		maxTurns = defaultMaxTurns
		if cfg.Subagents {
			// Deep mode adds an orchestration layer (dispatch + collect), so
			// give the main agent more head-room before the runaway cap.
			maxTurns = 80
		}
	}

	// Tool surface. Single-agent mode blocks Task (no subagents); deep mode
	// allows Task so the agent can fan out parallel per-threat investigations.
	// Either way the dangerous write/exec/network built-ins stay disallowed
	// session-wide (subagents inherit the block).
	allowed := cfg.AllowedTools
	denylist := readOnlyDenylistBase
	if cfg.Subagents {
		allowed += ",Task"
	} else {
		denylist += ",Task"
	}

	args := []string{
		"-p", prompt,
		"--mcp-config", mcpCfg,
		"--allowedTools", allowed,
		// stream-json gives the per-message envelope stream (cost, tokens,
		// tool calls, session_id) that text mode threw away. --verbose is
		// required for per-turn envelopes.
		"--output-format", "stream-json", "--verbose",
		"--max-turns", strconv.Itoa(maxTurns),
	}
	// Hard read-only enforcement: block code-exec / file-write / network
	// built-ins so the scan agent can only use the read-only assay_* tools,
	// regardless of the host's permission mode.
	if caps.DisallowedTools {
		args = append(args, "--disallowedTools", denylist)
	}
	// --bare isolates the scan from the host's CLAUDE.md, hooks, skills, and
	// installed plugins — reproducible scans that can't be biased or broken by
	// the user's own config. BUT in --bare mode Claude Code also skips keychain
	// reads and authenticates STRICTLY via ANTHROPIC_API_KEY (or an apiKeyHelper
	// passed through --settings). Assay is subscription-first: the default path
	// is the OAuth bearer Claude Code negotiated, which lives in the keychain.
	// Passing --bare there makes the subprocess report "Not logged in · Please
	// run /login" and exit 1 in ~12ms — every MCP-mode scan fails. So only
	// isolate with --bare when an API key is actually present (CI / legacy
	// API-key users); on the subscription path we accept reduced isolation in
	// exchange for a scan that can authenticate at all.
	if caps.Bare && cfg.APIKeyAuth {
		args = append(args, "--bare")
	}
	// --strict-mcp-config makes the subprocess use ONLY the assay server from
	// our --mcp-config, ignoring the host's other MCP servers (the user's
	// ~/.claude.json mcpServers, plugin-provided servers, etc.). Without it the
	// scan would fetch and load every MCP the user has configured — wasted
	// tokens, slower scans, and an unnecessary attack surface inside the scan
	// context. Pairs with --bare (which handles skills/hooks/plugins/CLAUDE.md).
	if caps.StrictMCP {
		args = append(args, "--strict-mcp-config")
	}
	// --model pins the Anthropic model the subprocess runs under. Empty means
	// "auto": let Claude Code pick the model the subscription allows (the only
	// safe default across Pro/Max tiers and across model retirements).
	if cfg.Model != "" {
		args = append(args, "--model", cfg.Model)
	}
	// --resume continues a prior session (diff-mode), reusing its context.
	if cfg.ResumeSessionID != "" {
		args = append(args, "--resume", cfg.ResumeSessionID)
	}
	args = append(args, cfg.ExtraArgs...)
	return args
}

// writeMCPConfig writes a one-shot MCP config file that tells Claude how to
// launch the assay stdio server. Tempfile is auto-removed by SpawnScan.
// mcpServersJSON returns the standard MCP-config bytes that tell any MCP client
// how to launch the assay stdio server. Shared by every agent adapter (Claude's
// --mcp-config file, Cursor's .cursor/mcp.json, …) — the config is identical;
// only where each CLI reads it from differs.
func mcpServersJSON(assayBin string, offline bool) []byte {
	args := []string{"mcp", "--transport", "stdio"}
	if offline {
		// Propagate offline to the MCP server subprocess so its deterministic
		// SCA floor (assembleVerdict → floor.Apply) skips the OSV.dev lookup.
		args = append(args, "--offline")
	}
	cfg := map[string]any{
		"mcpServers": map[string]any{
			"assay": map[string]any{
				"command": assayBin,
				"args":    args,
			},
		},
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	return data
}

func writeMCPConfig(assayBin string, offline bool) (string, error) {
	data := mcpServersJSON(assayBin, offline)
	tmpDir := os.TempDir()
	f, err := os.CreateTemp(tmpDir, "assay-mcp-*.json")
	if err != nil {
		return "", err
	}
	name := f.Name()
	defer func() { _ = f.Close() }()
	if _, err := f.Write(data); err != nil {
		// Remove the just-created temp file so a write failure (e.g. low disk)
		// doesn't leak it — SpawnScan's deferred os.Remove(mcpCfg) is a no-op
		// when we return "".
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

// buildMCPPrompt is the user-message Claude receives via `-p`. It tells the
// agent to load the assay_methodology prompt for the target — Claude Code
// resolves @-mentions to MCP prompts, so the agent fetches the embedded
// methodology and follows it. The scan_id is pre-allocated by the caller so
// we know where to tail events.jsonl from.
//
// When `offline` is true the prompt appends an explicit instruction to skip
// outbound network calls (notably assay_osv_lookup). This honors the
// `offline` flag end-to-end in MCP mode rather than letting it silently
// drop at the spawn boundary.
func buildMCPPrompt(target, scanID string, offline, subagents bool) string {
	offlineLine := ""
	if offline {
		offlineLine = "\n\nIMPORTANT: This scan is running in OFFLINE mode. Do NOT call assay_osv_lookup or any other tool that performs outbound network requests. Use only local static analysis and the read-only filesystem tools."
	}
	deepLine := ""
	if subagents {
		deepLine = fmt.Sprintf("\n\nDEEP MODE (parallel investigation): At the investigation step, do NOT investigate the threats one by one yourself. Instead dispatch each threat Tn as a SEPARATE parallel Task subagent. Give each subagent (a) the threat's description and reviewer questions, (b) the scan_id %q, (c) the target path %q, and (d) this instruction verbatim: \"Use ONLY the read-only assay_* tools (assay_read_file, assay_grep, assay_symbol_refs, assay_parse_manifest, assay_secret_scan) to investigate this single threat. Use assay_symbol_refs to trace a value source→sink (or prove a dangerous call unreachable) instead of chaining greps. For each CONFIRMED issue call assay_record_finding with the scan_id above and verbatim file:line evidence. Do not record speculative findings.\" Launch the subagents concurrently, wait for all to finish, then continue to exploitability + synthesis and call assay_finalize_scan as normal. Investigating each threat in its own subagent context keeps the analysis focused and avoids context dilution.", scanID, target)
	}
	return fmt.Sprintf(`Run a full Assay security scan against the target below.

Target: %s
Pre-allocated scan_id: %s

To drive the scan, call assay_scan_start with that target and scan_id (the server will reuse the allocated directory). Then load the prompt @assay-mcp:assay_methodology with target=%q and follow EVERY step in order, calling the assay_* tools at each stage. Do not skip the finalize step.

When assay_finalize_scan returns, report the verdict, the surviving-findings count, and the scan_dir to me. That's it.%s%s`,
		target, scanID, target, offlineLine, deepLine)
}

// EnsureAbsolutePath helps callers reject relative targets cleanly before
// spawning. Reused by tests + the FE-pivot StartScanFunc.
func EnsureAbsolutePath(p string) error {
	if !filepath.IsAbs(p) {
		return fmt.Errorf("target must be an absolute path, got %q", p)
	}
	return nil
}
