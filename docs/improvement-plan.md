# Assay Improvement Plan

Compiled 2026-05-29 from a five-agent codebase audit (Claude Code integration,
detection depth, web UI/API, product gaps, reliability/security) plus a
deep-research pass over primary sources (25 sources, 114 claims extracted, 25
adversarially verified — 17 confirmed, 8 refuted). Citations for external
claims are in the References section.

## Hard constraint (do not re-litigate)

The reasoning engine is **Claude Code on the user's subscription**, driven via
the `claude -p` CLI subprocess. Never the Anthropic Messages API and never the
Claude Agent SDK (Python/TS) as the primary path:

1. Most users have a Claude Code subscription, not an API key. Running on
   subscription quota (no key, no per-token billing, no 429s) is the moat.
2. `claude -p` IS Claude Code — it uses the user's login. It is not the API.
3. The legacy Go Messages-API path (`internal/scanner`) stays a fallback only,
   for CI/headless boxes with no Claude Code login.
4. The Agent SDK is Python/TS and would break the single-Go-binary install; the
   research verification round also *killed* the claims that justified it (see
   References — "refuted").

All engine improvements deepen what `claude -p` does, on the subscription.

## Why the SDK migration was rejected (research finding)

The deep-research summary led with "migrate to the Claude Agent SDK with
parallel subagents." Verification undercut it:

1. "parallel subagents native by default" — refuted 1-2.
2. "two-agent initializer/coder harness is superior" — refuted 0-3.
3. "structured state files are necessary for continuity" — refuted 0-3.
4. "SDK auto-compaction is sufficient" — refuted 1-2.
5. Open question with no answer: whether an SDK subagent can even spawn the
   assay MCP stdio server the way `claude -p --mcp-config` does.

Everything that survived 3-0 is a `claude -p` CLI flag that runs on the
subscription. The per-subagent model + tool-scoping capability the SDK was
praised for is also reachable through Claude Code's **native** subagents
(`.claude/agents` + Task tool). So: keep `claude -p`, use more of its surface.

---

## Phase 1 — Stop the bleeding (SHIPPED 2026-05-29)

All fixes below are merged with regression tests; `go test ./...`,
`go vet ./...`, and `go test -race` on the concurrency-touched packages all
pass.

1. **QA-T8 actually fixed** (`internal/claude/agent.go`, `client.go`). The
   legacy agent loop now reconstructs the assistant turn (text + tool_use
   blocks) before the user/tool_result turn, so the transcript alternates
   user/assistant/user and every tool_result references a tool_use_id present
   in the prior assistant turn. This required extending the `Content` type with
   `Name`/`Input` and adding a `tool_use` case to the real-client serializer
   (the audit's one-line suggestion would not have worked). Test:
   `TestAgentReconstructsAssistantTurnBeforeToolResult`.
2. **Fleet deadlock fixed** (`internal/fleet/runner.go`). A member whose scan
   crashes before writing its terminal `done` event can no longer wedge
   `Runner.Run` forever. Tailers now run on a cancelable context that is
   force-canceled a 2s grace window after all workers finish. Test:
   `TestRunnerCompletesWhenMemberWritesNoDoneEvent`.
3. **Scan-runner map purge** (`internal/api/scan_runner.go`). `Complete` now
   deletes the scan's entry from the `active` map instead of merely marking it
   closed — fixes unbounded memory growth on a long-lived `assay serve`. Test:
   `TestScanRunnerCompletePurgesActiveMap`.
4. **Temp-file leak fixed** (`internal/mcp/spawn.go`). `writeMCPConfig` removes
   the temp file when the write fails (previously leaked because SpawnScan's
   deferred `os.Remove("")` is a no-op).
5. **Path-guard tightened** (`internal/api/pathguard.go`). `os.TempDir()`
   removed from `DefaultAllowedRoots` — on macOS the temp tree is
   world-writable, so trusting it let a local process plant a target at a
   predictable path and have serve read+exfiltrate it. Test:
   `TestDefaultAllowedRootsExcludesTempDir`.
6. **SCA respects cancellation** (`internal/mcp/verdict_assemble.go`). The OSV
   round-trip derives its timeout from the caller's context instead of
   `context.Background()`, so a cancelled/shutting-down scan stops the HTTP
   work.
7. **ReadFile DoS guard** (`internal/tools/fs.go`). Files over 10 MiB return a
   guard message instead of being loaded fully into the heap. Test:
   `TestReadFileRejectsHugeFile`.
8. **Pre-install hook hardened** (`plugin/hooks/assay-pre-install.sh`).
   Portable `run_with_timeout` (GNU `timeout` → `gtimeout` → uncapped) — the
   gate was a silent no-op on stock macOS, which has no `timeout` binary. Plus
   a loud stderr warning when python3 is missing instead of silent fail-open.
9. **Default model → "auto"** (`internal/store/config.go`). `Models.Default`
   and `.Investigation` now default to `""` so Claude Code picks the model the
   subscription allows; pinning a version string was a latent availability bomb
   on model retirement. The legacy API path substitutes `scanner.DefaultModel`
   when empty (the Messages API needs a concrete model). Test updated.
10. **`--max-turns` + scan timeout** (`internal/mcp/spawn.go`). `claude -p` now
    runs with `--max-turns` (default 50) and a 15-minute wall-clock deadline
    via `context.WithTimeout`, so a stalled/looping subprocess can no longer run
    forever on the user's quota. Arg construction extracted into a testable
    `buildClaudeArgs`. Test: `TestBuildClaudeArgsIncludesMaxTurnsAndModel`.

Still queued from the reliability audit (not yet done): fleet goroutines not
canceled on SIGTERM (`api/fleet.go`), fleet ID unvalidated in the stream
handler (`api/fleet.go`).

---

## Phase 2 — CI unlock (PARTIALLY SHIPPED 2026-05-29)

The single largest adoption blocker: `assay scan` exited 0 regardless of
verdict, so every CI integration was ornamental. Shipped items are merged with
tests; `go build`/`go vet`/`go test ./...` pass.

1. **Non-zero exit codes (SHIPPED)** — `--fail-on <unsafe|caution|any|off>`
   (default `unsafe`) on both `assay scan` and `assay scan-all`. Exit code 2 on
   threshold, distinct from 1 = crash, via an `exitCodeError` honored in
   `main.go`. Tests: `TestEvalFailOn`, `TestEvalFleetFailOn`.
2. **SARIF 2.1 output (SHIPPED)** — `--format sarif` writes `audit.sarif`
   alongside `audit.json` (and `--output` copies it). `internal/verdict/sarif.go`
   maps `evidence[].file`/`.line` to SARIF `physicalLocation`; severities map
   critical/high→error, medium→warning, low/info→note; findings with no
   evidence get a fallback root location so GitHub still renders them. Test:
   `TestToSARIF`.
3. **Deterministic floor extracted + wired into legacy/CLI (SHIPPED)** — new
   `internal/floor` package applies SCA + poison; `internal/mcp/verdict_assemble.go`
   and the legacy CLI path (`cmd_scan.go`) now both call it, closing the gap
   where `--scan-mode legacy`/CLI users got a weaker audit. (`--quick` floor
   wiring deferred — see below.) Tests in `internal/floor`.
4. **`assay-action` composite GitHub Action (SHIPPED, pre-release)** —
   `/action.yml`: installs the binary, runs a quick (no-secret, deterministic)
   or full (API-key legacy lane) scan, gates with `--fail-on`, and uploads
   SARIF via `github/codeql-action/upload-sarif`. Functional once releases are
   published.

Deferred to a follow-up (not blocking):

5. **`--quick` floor + gate** — wiring SCA/poison into the <2s pre-install
   gate needs re-scoring inside `scanner` (and SCA's OSV network call would
   blow the <2s budget), so the Action's quick mode gates on pre-pass risk for
   now. Add poison (local, fast) to the quick path; keep SCA out of it.
6. **OWASP finding-ID tagging** — tag findings with OWASP MCP Top 10
   (MCP01–MCP10) and Agentic Top 10 (ASI01…) IDs; version the mapping (beta).
7. **PR-comment bot** in the Action (markdown summary of `audit.md`).
8. **Scan list / history verdict badges** (`scans.go` list payload + FE).

## Phase 3 — Engine keystone (PARTIALLY SHIPPED 2026-05-29)

`stream-json` is the keystone: it unlocks cost metering, stall detection, and
session resume. Shipped items are merged with tests; build/vet/test/race pass.

1. **stream-json consumption (SHIPPED)** — `spawn.go` now runs
   `--output-format stream-json --verbose` and consumes stdout (was
   `io.Discard`) via a pure, unit-tested `parseStreamJSON`. Surfaces token
   usage, cumulative cost, tool-call names, and the `session_id` through a new
   `SpawnConfig.OnStreamEvent` callback. The serve path emits a `scan cost $X`
   progress event on the terminal result. Tests: `TestParseStreamJSON`.
2. **`--bare` (SHIPPED, capability-gated)** — isolates the scan from the host's
   `CLAUDE.md`/hooks/skills/plugins. Confirmed by probe that `--bare` preserves
   the explicit `--mcp-config` (the assay MCP server still loads). Added only
   when `claude --help` advertises it (`claudeCapabilities`, cached), so older
   CLIs degrade gracefully.
3. **Read-only enforcement (SHIPPED — replaces the bad "dontAsk" idea)** — the
   research's `--permission-mode dontAsk` value does NOT exist in claude
   2.1.156 (valid: acceptEdits/auto/bypassPermissions/default), and under
   `--bare` the session was observed in `bypassPermissions`. So instead of a
   permission mode, `spawn.go` passes `--disallowedTools
   Bash,Edit,Write,NotebookEdit,WebFetch,WebSearch,Task` — a hard block that
   keeps the scan agent read-only (no code exec, file write, or network exfil)
   regardless of permission mode. Capability-gated. Tests:
   `TestBuildClaudeArgsCapabilityGating`.
4. **`--resume` for diff-mode (SHIPPED)** — `SpawnConfig.ResumeSessionID` →
   `--resume`; the serve path persists each scan's `session.json` and, on a
   diff-mode (`since`) scan, loads the prior scan's session id to resume it.
   Best-effort (cold scan on any miss).

5. **Parallel per-threat subagents (SHIPPED, opt-in) — partial.** A new
   `scan.deep_scan` config flag (default false; `assay config set
   scan.deep_scan true`) turns on deep mode in `spawn.go`: the agent dispatches
   each Stage-5 threat investigation as a parallel Claude Code `Task` subagent
   (its own context window → less dilution, deeper per-threat analysis). When
   on, `--allowedTools` gains `Task`; when off, `Task` stays in the
   `--disallowedTools` block. Either way the dangerous write/exec/network
   built-ins remain disallowed session-wide, so subagents stay read-only too.
   Deep mode also raises the default `--max-turns` to 80 for the orchestration
   layer. The deep-mode instruction (carrying the scan_id so subagents can
   record findings) is injected into the per-scan prompt. Tests:
   `TestBuildClaudeArgsSubagentsGating`, `TestBuildMCPPromptDeepMode`. Opt-in
   because it spends more of the user's quota, and because subagent MCP-tool
   propagation should be live-validated under a logged-in `claude`.

   **Per-stage model routing (still deferred — architectural reason).** A single
   `claude -p` runs ONE `--model` with no per-turn switch, so true per-stage
   models (Haiku triage / Opus investigation) require splitting the scan into
   multiple subprocesses (cheap triage subprocess → deep investigation
   subprocess), passing the threat model between them via a new
   `assay_record_threats`/load tool. That is a distinct pipeline change; the
   `models.investigation` config field is the groundwork. Next increment.
6. **Frontend wiring (SHIPPED) + richer live-scan UI (partial).** The Settings
   page now has an editable **deep-scan toggle** (PUT /api/config) and an
   **"Auto" model option** (so the Phase-1 `""` default renders correctly
   instead of mis-showing Opus). Config round-trip validated through the real
   backend (GET → PUT with `X-Assay-CSRF: 1` → GET reflects `deep_scan:true` +
   `models.default:""`; CSRF enforced with 403 on a header-less PUT). `assay
   config set scan.deep_scan true` also works. Live-scan cost already surfaces
   as a `scan cost $X` progress message; a dedicated cost chip + per-tool-call
   rendering is the remaining FE polish.

Runtime caveat: end-to-end validation against a logged-in `claude` is still
recommended — this machine's test shell was not logged in, so the full scan
path (tool calls completing) was validated by the init envelope + unit tests,
not a full live scan.

## Phase 4 — Detection depth + trust (the accuracy that earns the verdict)

**Shipped 2026-05-29 (deterministic-floor batch — runs in every scan mode via
`internal/floor`; all unit-tested):**

* **Go + Rust SCA** (`internal/sca`) — `go.sum` → OSV `Go`, `Cargo.lock` → OSV
  `crates.io`. Closes a real blind spot (many AI-dev tools are Go/Rust). Tests:
  `TestWalkManifestsParsesGoSum`, `TestWalkManifestsParsesCargoLock`.
* **POISON-006 fixed** (`internal/poison`) — the typosquat detector was a dead
  stub that always returned nil; now it extracts declared tool/command `name`s
  from the manifest JSON and flags any that are edit-distance-1 from a curated
  well-known name (`gitt`→`git`), while leaving exact and distant names alone.
  Tests: `TestScanCatchesTypoSquatName`, `TestScanDoesNotFlagExactOrDistantNames`,
  `TestIsDistance1`.
* **POISON-007 oversized description** — gives T8 (cost-bleed) / "context
  overflow" the deterministic signal it was missing: flags any manifest string
  field > 2000 chars (recurring token cost + a hiding place for injected
  directives). Test: `TestScanCatchesOversizedDescription`.
* **Grep truncation signal + dependency-tree skip** (`internal/tools/fs.go`) —
  grep now emits `[TRUNCATED…]` when it hits the 50-match cap (so a partial
  result isn't mistaken for "all"), and skips `node_modules`/`vendor` so
  bundled deps don't push the plugin's own files past the cap. Tests:
  `TestGrepSignalsTruncation`, `TestGrepSkipsNodeModules`.
* **`Finding.Source` enum** (`internal/verdict`) — findings are tagged `llm` /
  `sca` / `poison`. The citation validator now *intentionally and auditably*
  exempts the deterministic-floor sources (whose evidence is a synthetic
  manifest reference, not a source-code citation) from the file re-read, instead
  of relying on a synthetic snippet happening to match. Closes the audited
  validator-bypass. Schema updated. Test:
  `TestValidateExemptsDeterministicFloorSources`.
* **Summary-honesty fix** (`internal/mcp/verdict_assemble.go`) — when the
  deterministic floor raises the verdict above the LLM's own assessment, the
  executive summary is prefixed with a disclosure ("Assay raised this verdict
  to unsafe via its … floor after the LLM review …") so the summary can't read
  "safe" while CVEs/poison sit in the findings. Tests:
  `TestAssembleVerdictSummaryHonestyOnFloorUpgrade`, `TestAssembleVerdictNoNoteWhenClean`.
* **Policy-as-code** (`internal/policy`, `.assay-policy.json`) — `suppress`
  (reviewed/accepted findings; **reason required**, optional `expiry` so
  suppressions can't silently outlive their rationale), `deny_categories` (fail
  the gate on a category regardless of severity), `allowlist` (skip trusted
  targets), and a default `fail_on`. Wired into `assay scan` via `--policy`
  (precedence: `--policy` flag > `./.assay-policy.json` in cwd). **Security:**
  the policy is the scanning user's — resolved from the flag/cwd, NEVER from the
  target tree, so a malicious plugin can't ship a policy to suppress its own
  findings. A malformed policy is a hard error (visible, not silently ignored).
  JSON schema at `schemas/policy-v0.1.json`. 11 unit tests in `internal/policy`;
  allowlist-skip + malformed-policy-error verified end-to-end with the binary.

**Remaining Phase 4 (not yet done):**

1. **Seed the data-flow diagram with pre-pass hits** (methodology Step 2.5).
   The diagram drives the whole threat model but is built from manifests +
   README only — so an obfuscated plugin (README lies) gets no credential-read
   node. Run secret_scan + targeted grep before the diagram. Prompt-only, big
   FN reduction.
2. ~~**`assay_symbol_refs` tool**~~ — **SHIPPED 2026-05-29.** Deterministic
   cross-file symbol locator: returns a symbol's definition(s) and every
   reference in one call (classified def-vs-ref heuristically), so the model
   traces a value's source→sink without chaining grep+read_file, and can prove
   non-reachability to kill FPs. Word-boundary matching (no `token` ⊂
   `tokenizer`), skips node_modules/vendor, 60-hit cap with truncation signal.
   Wired into `internal/tools` (`FS.SymbolRefs`), the MCP server
   (`assay_symbol_refs`), the legacy scanner's sub-agent toolset, and named in
   the methodology's investigation step. Tests:
   `TestSymbolRefsClassifiesDefsAndRefs`, `TestSymbolRefsWordBoundary`,
   `TestSymbolRefsNoMatch`, `TestSymbolRefsRequiresSymbol`.
3. **Adversarial verification pass (LLM-as-judge)** — an independent pass that
   sees only the final audit.json and is prompted to refute each finding;
   downgrade the refuted. Use a cheap (Haiku-tier) refuter. Biggest FP reducer.
4. **Always-on regex prompt-injection baseline** (Cisco-style) — ~12
   attack-vector rules (Instruction Override, Data Leakage, Role Escape,
   Indirect Injection, Output Weaponization, Multilingual Bypass,
   Unicode/Homoglyph, Context Overflow, …) that always run, no API key,
   instant; pre-filters known-bad before the Claude subprocess (cuts token
   cost).
5. **Deterministic floors for LLM-only classes** — hook-abuse pre-pass (flag
   wildcard `UserPromptSubmit`/`SessionStart` matchers, `curl`/`nc` in hook
   bodies via the existing `inventory.ReadHooks`), cost-bleed pre-pass (>2 KB
   tool descriptions), and an `assay settings-audit` command for T7 (today:
   zero detection).
6. **Integrity fixes** — implement or remove the dead POISON-006 typosquat stub
   (`poison.go`); add a `Source` enum so SCA evidence bypassing the citation
   validator is intentional/auditable; prepend an override notice when the SCA
   floor upgrades a verdict the LLM-written summary still calls "safe"; add
   Go/Rust SCA (`go.sum`, `Cargo.lock` → OSV `Go`/`crates.io`).
7. **Signed verdicts** — the `Signatures` field exists but is always nil.
   cosign keyless `sign-blob` (already a goreleaser dep) + an `assay verify`
   subcommand. Prerequisite for the Ring-1 community verdict DB.

   (Policy-as-code shipped — see the Shipped block above. Follow-up: wire the
   same `internal/policy` suppression/deny into the MCP/serve scan path, not
   just the CLI.)

## Phase 5 — The masterpiece bet (no competitor has this)

**Cross-plugin confused-deputy chain analysis.** Individual-plugin scanning is
table stakes; reasoning about whether two plugins the user trusts individually
compose into an exfil chain (reader primitive in A + outbound-write in B) is
the novel value prop. Build on the fleet artifacts already produced: capability
graph from each `triage.json`/`audit.json` → one Sonnet pass →
`chain-report.json`. Already in TODO as "the next big leverage feature."

---

## Don't build (refuted or out of scope)

1. Two-agent initializer/coder harness — refuted 0-3.
2. Reliance on auto-compaction for large repos — uncertain; use `--resume` +
   per-threat subagents instead.
3. Claude Agent SDK migration — off-constraint and weakly evidenced.

## Open questions to decide before Phase 3/4

1. Subagent cost model — 7–10 Sonnet subagents vs one Opus run, on the user's
   quota, across fleet scans of dozens of plugins. Needs measurement; make
   depth a setting and meter it.
2. SARIF location representation for non-code findings (tool-description text /
   manifest fields).

## References (primary sources, adversarially verified)

1. Claude Code headless docs — stream-json, `--resume`, `--bare`,
   `--permission-mode`: https://code.claude.com/docs/en/headless
2. Claude Agent SDK subagents — per-subagent tools/model:
   https://code.claude.com/docs/en/agent-sdk/subagents
3. Agent SDK streaming output — streaming XOR extended thinking:
   https://code.claude.com/docs/en/agent-sdk/streaming-output
4. Building agents with the Claude Agent SDK:
   https://www.anthropic.com/engineering/building-agents-with-the-claude-agent-sdk
5. Effective harnesses for long-running agents:
   https://www.anthropic.com/engineering/effective-harnesses-for-long-running-agents
6. Cisco open-source MCP Scanner — 3 engines + 12 regex prompt-defense rules:
   https://github.com/cisco-ai-defense/mcp-scanner
7. OWASP MCP Top 10 (beta) — MCP03 Tool Poisoning, MCP09 Shadow MCP Servers:
   https://owasp.org/www-project-mcp-top-10/
8. OWASP Top 10 for Agentic Applications 2026 — ASI01 Agent Goal Hijack,
   EchoLeak (CVE-2025-32711, CVSS 9.3):
   https://genai.owasp.org/resource/owasp-top-10-for-agentic-applications-for-2026/
9. GitHub SARIF support for code scanning:
   https://docs.github.com/en/code-security/code-scanning/integrating-with-code-scanning/sarif-support-for-code-scanning
10. Google Project Zero — From Naptime to Big Sleep:
    https://projectzero.google/2024/10/from-naptime-to-big-sleep.html
11. XBOW — autonomous attacks at scale:
    https://xbow.com/blog/we-ran-1060-autonomous-attacks
