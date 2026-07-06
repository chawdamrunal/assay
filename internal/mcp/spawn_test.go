package mcp

import (
	"strings"
	"testing"
)

// TestParseStreamJSON guards the stream-json consumer: it must extract the
// session id (for --resume), the terminal cost, tool-call names, and survive
// interleaved non-JSON banner lines.
func TestParseStreamJSON(t *testing.T) {
	stream := strings.Join([]string{
		`starting up...`, // non-JSON banner — must be skipped
		`{"type":"system","subtype":"init","session_id":"sess-abc","mcp_servers":[{"name":"assay"}]}`,
		`{"type":"assistant","session_id":"sess-abc","message":{"usage":{"input_tokens":120,"output_tokens":30},"content":[{"type":"tool_use","name":"assay_read_file"},{"type":"text","text":"reading"}]}}`,
		``, // blank line
		`{"type":"result","subtype":"success","session_id":"sess-abc","total_cost_usd":0.0421,"is_error":false}`,
	}, "\n")

	var events []StreamEvent
	sid := parseStreamJSON(strings.NewReader(stream), func(e StreamEvent) {
		events = append(events, e)
	})

	if sid != "sess-abc" {
		t.Fatalf("expected session id sess-abc, got %q", sid)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 parsed envelopes (banner+blank skipped), got %d", len(events))
	}

	// The assistant envelope surfaces the tool name and usage.
	var sawToolUse bool
	var finalCost float64
	for _, e := range events {
		for _, n := range e.ToolNames {
			if n == "assay_read_file" {
				sawToolUse = true
			}
		}
		if e.Type == "result" {
			finalCost = e.CostUSD
		}
	}
	if !sawToolUse {
		t.Fatalf("expected to surface the assay_read_file tool_use, events: %+v", events)
	}
	if finalCost != 0.0421 {
		t.Fatalf("expected terminal cost 0.0421, got %v", finalCost)
	}
}

// TestBuildMCPPromptHonorsOffline regression-guards the v0.5.1 fix for the
// silently-dropped --offline flag. When SpawnConfig.Offline=true, the prompt
// must explicitly tell Claude not to call assay_osv_lookup.
func TestBuildMCPPromptHonorsOffline(t *testing.T) {
	online := buildMCPPrompt("/some/target", "scan-1", false, false)
	offline := buildMCPPrompt("/some/target", "scan-1", true, false)

	if strings.Contains(online, "OFFLINE") {
		t.Fatalf("online prompt unexpectedly contains OFFLINE directive:\n%s", online)
	}
	if !strings.Contains(offline, "OFFLINE") {
		t.Fatalf("offline prompt missing OFFLINE directive:\n%s", offline)
	}
	if !strings.Contains(offline, "assay_osv_lookup") {
		t.Fatalf("offline prompt should name the network tool by id; got:\n%s", offline)
	}
}

// TestBuildClaudeArgsIncludesMaxTurnsAndModel guards core arg behavior: the
// runaway-loop cap is always passed (--max-turns); the keystone stream-json
// output is always set; and --model is only passed when explicitly pinned
// (empty = let Claude Code pick the subscription model).
func TestBuildClaudeArgsIncludesMaxTurnsAndModel(t *testing.T) {
	// Empty model → no --model flag (auto), default max-turns + stream-json.
	args := buildClaudeArgs(SpawnConfig{AllowedTools: "mcp__assay__*"}, "/tmp/mcp.json", "prompt", claudeCaps{})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--max-turns 50") {
		t.Fatalf("expected default --max-turns 50, got: %s", joined)
	}
	if !strings.Contains(joined, "--output-format stream-json --verbose") {
		t.Fatalf("expected stream-json output, got: %s", joined)
	}
	if strings.Contains(joined, "--model") {
		t.Fatalf("empty model must NOT emit --model (auto), got: %s", joined)
	}

	// Pinned model + custom max-turns are both honored.
	args = buildClaudeArgs(SpawnConfig{AllowedTools: "mcp__assay__*", Model: "claude-opus-4-7", MaxTurns: 12}, "/tmp/mcp.json", "prompt", claudeCaps{})
	joined = strings.Join(args, " ")
	if !strings.Contains(joined, "--model claude-opus-4-7") {
		t.Fatalf("pinned model must emit --model, got: %s", joined)
	}
	if !strings.Contains(joined, "--max-turns 12") {
		t.Fatalf("custom max-turns must be honored, got: %s", joined)
	}
}

// TestBuildClaudeArgsCapabilityGating guards that --bare and --disallowedTools
// are emitted only when the installed claude supports them, and that --resume
// is emitted only when a session id is set.
func TestBuildClaudeArgsCapabilityGating(t *testing.T) {
	// No capabilities → none of the optional isolation flags.
	bare := buildClaudeArgs(SpawnConfig{AllowedTools: "mcp__assay__*"}, "/c.json", "p", claudeCaps{})
	j := strings.Join(bare, " ")
	if strings.Contains(j, "--bare") || strings.Contains(j, "--disallowedTools") || strings.Contains(j, "--strict-mcp-config") {
		t.Fatalf("no-caps args must omit --bare/--disallowedTools/--strict-mcp-config, got: %s", j)
	}
	if strings.Contains(j, "--resume") {
		t.Fatalf("no resume id must omit --resume, got: %s", j)
	}

	// Full capabilities + resume id + API-key auth → all present, denylist
	// enforced. --bare requires APIKeyAuth because --bare authenticates strictly
	// via ANTHROPIC_API_KEY (it skips keychain reads).
	full := buildClaudeArgs(
		SpawnConfig{AllowedTools: "mcp__assay__*", ResumeSessionID: "sess-123", APIKeyAuth: true},
		"/c.json", "p",
		claudeCaps{Bare: true, DisallowedTools: true, StrictMCP: true},
	)
	j = strings.Join(full, " ")
	if !strings.Contains(j, "--bare") {
		t.Fatalf("caps.Bare + APIKeyAuth must emit --bare, got: %s", j)
	}
	if !strings.Contains(j, "--strict-mcp-config") {
		t.Fatalf("caps.StrictMCP must emit --strict-mcp-config (isolate from host MCP servers), got: %s", j)
	}
	if !strings.Contains(j, "--disallowedTools "+readOnlyDenylistBase+",Task") {
		t.Fatalf("single-agent mode must disallow the read-only denylist plus Task, got: %s", j)
	}
	if !strings.Contains(j, "--resume sess-123") {
		t.Fatalf("resume id must emit --resume, got: %s", j)
	}

	// Regression (subscription path): caps.Bare is true but no API key is
	// present, so --bare must be OMITTED. In --bare mode Claude Code skips
	// keychain reads and demands ANTHROPIC_API_KEY, so emitting it on the OAuth
	// subscription path makes every scan fail instantly with "Not logged in".
	// The rest of the isolation flags (--strict-mcp-config) must still apply.
	sub := strings.Join(buildClaudeArgs(
		SpawnConfig{AllowedTools: "mcp__assay__*"}, // APIKeyAuth zero-value: false
		"/c.json", "p",
		claudeCaps{Bare: true, DisallowedTools: true, StrictMCP: true},
	), " ")
	if strings.Contains(sub, "--bare") {
		t.Fatalf("subscription path (no API key) must NOT emit --bare — it breaks OAuth keychain auth, got: %s", sub)
	}
	if !strings.Contains(sub, "--strict-mcp-config") {
		t.Fatalf("subscription path must still emit --strict-mcp-config, got: %s", sub)
	}
}

// TestBuildClaudeArgsSubagentsGating guards deep mode: Task is allowed (so the
// agent can fan out parallel investigations) and removed from the denylist,
// while the dangerous write/exec/network built-ins stay disallowed.
func TestBuildClaudeArgsSubagentsGating(t *testing.T) {
	// Single-agent (default): Task blocked, not allowed.
	solo := strings.Join(buildClaudeArgs(
		SpawnConfig{AllowedTools: "mcp__assay__*"}, "/c.json", "p",
		claudeCaps{DisallowedTools: true},
	), " ")
	if strings.Contains(solo, "mcp__assay__*,Task") {
		t.Fatalf("single-agent mode must NOT allow Task, got: %s", solo)
	}
	if !strings.Contains(solo, ",Task") { // present in the denylist
		t.Fatalf("single-agent mode must disallow Task, got: %s", solo)
	}

	// Deep mode: Task allowed, denylist keeps the dangerous built-ins.
	deep := strings.Join(buildClaudeArgs(
		SpawnConfig{AllowedTools: "mcp__assay__*", Subagents: true}, "/c.json", "p",
		claudeCaps{DisallowedTools: true},
	), " ")
	if !strings.Contains(deep, "--allowedTools mcp__assay__*,Task") {
		t.Fatalf("deep mode must allow Task, got: %s", deep)
	}
	if !strings.Contains(deep, "--disallowedTools "+readOnlyDenylistBase) {
		t.Fatalf("deep mode must still disallow write/exec/network built-ins, got: %s", deep)
	}
	if strings.Contains(deep, readOnlyDenylistBase+",Task") {
		t.Fatalf("deep mode must NOT disallow Task, got: %s", deep)
	}
	if !strings.Contains(deep, "--max-turns 80") {
		t.Fatalf("deep mode should raise the default turn cap to 80, got: %s", deep)
	}
}

// TestBuildMCPPromptHasTargetAndScanID is a smoke test against the two
// invariant fields the methodology relies on.
func TestBuildMCPPromptHasTargetAndScanID(t *testing.T) {
	out := buildMCPPrompt("/abs/path/plugin", "abcd-1234", false, false)
	if !strings.Contains(out, "/abs/path/plugin") {
		t.Fatalf("target missing from prompt:\n%s", out)
	}
	if !strings.Contains(out, "abcd-1234") {
		t.Fatalf("scan_id missing from prompt:\n%s", out)
	}
	if strings.Contains(out, "DEEP MODE") {
		t.Fatalf("non-deep prompt must not contain the deep-mode block:\n%s", out)
	}
}

// TestBuildMCPPromptDeepMode guards that deep mode injects the parallel-subagent
// instruction carrying the scan_id (subagents need it to record findings).
func TestBuildMCPPromptDeepMode(t *testing.T) {
	out := buildMCPPrompt("/abs/plugin", "scan-xyz", false, true)
	if !strings.Contains(out, "DEEP MODE") {
		t.Fatalf("deep prompt must contain the deep-mode block:\n%s", out)
	}
	if !strings.Contains(out, "Task subagent") {
		t.Fatalf("deep prompt must instruct dispatching Task subagents:\n%s", out)
	}
	if !strings.Contains(out, "scan-xyz") {
		t.Fatalf("deep prompt must carry the scan_id so subagents can record findings:\n%s", out)
	}
}
