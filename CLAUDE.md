# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What Assay is

Assay is a security scanner for the AI dev stack (Claude Code plugins, MCP servers, hooks, settings). It reasons about an artifact with an LLM ("LLM as judge, not janitor") rather than pattern-matching. It compiles to a **single Go binary** (`bin/assay`) with the React SPA embedded via `embed.FS`. The Go module is `github.com/chawdamrunal/assay`.

The default scan path drives the reasoning through the user's **Claude Code subscription**, not the Anthropic API — this is a hard product constraint (see `README.md` "How scans run"). The API path exists only as a `--scan-mode legacy` fallback.

## Commands

Build, test, and lint go through the Makefile. Note it hardcodes `GO := /opt/homebrew/opt/go/bin/go` and expects `golangci-lint` and `pnpm` (via `corepack enable`) on PATH — `golangci-lint` is often not installed by default, so verify it locally before relying on `make lint`. Keep the Go-version pins in `go.mod`, the Makefile, and `.github/workflows/` in sync before pushing.

```bash
make build     # builds the SPA, copies it to internal/api/dist, then compiles bin/assay
make test      # go test -race ./...  AND  cd web && pnpm lint (tsc --noEmit)
make lint      # golangci-lint run ./...  AND  pnpm lint
make install   # copies bin/assay to ~/.local/bin (override PREFIX=/usr/local)
```

`make build` runs `make web` first (`pnpm install --frozen-lockfile && pnpm build`), which is required because the Go binary embeds `internal/api/dist/index.html` — `verify-embed` fails the build if the embed copy didn't land.

Running a single Go test:

```bash
/opt/homebrew/opt/go/bin/go test -race ./internal/scanner/ -run TestName
/opt/homebrew/opt/go/bin/go test -race ./internal/verdict/ -run TestValidator/subtest
```

### Three test suites, three cost profiles (build tags)

- **Default** — `go test ./...`. All Sonnet calls use `internal/claude/fake.go` `FakeClient`. Zero API cost. Gates every merge.
- **Integration** — `go test -tags integration ./...`. Replays recorded responses from `testdata/recorded/` against the golden corpus. Still zero API cost, full pipeline end-to-end.
- **Smoke** — `go test -tags smoke ./...`. Hits the real Anthropic API, needs `ANTHROPIC_API_KEY`, ~$0.20/run. **Excluded from CI**; run manually before release. Lives in `internal/scanner/realapi_test.go`.

The **golden corpus** in `testdata/` (intentionally-vulnerable artifacts that MUST flag + known-good ones that MUST NOT) exists to catch prompt drift. Frontend tests: `cd web && pnpm test` (vitest); `pnpm lint` is a typecheck (`tsc --noEmit`).

## Architecture

### One binary, three roles

`cmd/assay/` is a Cobra app whose subcommands select the role:
1. **CLI** — `version`, `config`, `inventory`, `scan`, `scan-all`, `auth`, `hook`.
2. **Web server** — `assay serve` binds `127.0.0.1:7373`, serves the embedded SPA + JSON/SSE API (`internal/api/`).
3. **MCP server** — `assay mcp --transport stdio` exposes the `assay_*` scanner toolset (`internal/mcp/`) that Claude Code drives.

The three roles are load-bearing together: `assay serve` (role 2) spawns `claude -p` with `--mcp-config` pointed back at `assay mcp` (role 3). The two processes share nothing in memory — an on-disk `events.jsonl` is the only coupling point.

### Two orchestrators, one verdict format

Both paths produce the **same `audit.json` + `audit.md` on disk**:

- **MCP mode (default)** — the agent loop runs *inside* Claude Code (via `/assay-scan` or `assay serve` spawning `claude -p`). Claude reads `internal/mcp/methodology.md` and chains `assay_*` tool calls. `mcp.SpawnScan` (`internal/mcp/spawn.go`) builds the `claude -p` invocation; `assay_finalize_scan` (`internal/mcp/verdict_assemble.go`) validates + applies the floor + renders the verdict.
- **Legacy in-process mode (`--scan-mode legacy`)** — the Go orchestrator in `internal/scanner/orchestrator.go` calls the Anthropic SDK directly, wrapped by the 429-retry (`internal/claude/retry.go`) and budget cap (`internal/claude/budget.go`). `--scan-mode fake` replays fixtures through this same orchestrator against `internal/claude/fake.go`.

The rest of the pipeline (citation validation, floor, policy, SSE, web UI, schema) is identical across modes.

### The 5-stage pipeline (both modes follow this methodology)

`pre-pass → Triage → Claim extraction → Threat model → Investigation → Exploitability → Synthesis → floor → validate+policy`

- **Pre-pass** (`internal/prepass/`, deterministic, no network) — high-precision secret regexes + suspicious-pattern flags + manifest discovery. Surfaces hints only; never produces final verdicts. Does NOT query OSV (so `--quick`/`--offline` have no dependency-CVE coverage).
- **Threat model is generated BEFORE the agent reads source code** — this ordering is the IP and is load-bearing. Threats map to the 12-class taxonomy (T1–T12, including MCP tool-poisoning, skill grant-abuse, and connector OAuth-scope classes) in `docs/threat-model-2026.md`.
- **Investigation** dispatches one parallel sub-agent per threat (default concurrency 3), each in a fresh context.
- **Synthesis verdict is arithmetic, not asked of the model** — `ComputeVerdict()` in `synthesis.go`: any critical/high → `unsafe`; any medium → `caution`; ≥3 low/info → `caution`; else `safe`.

### The deterministic floor (raises verdicts, never lowers)

Applied by the caller after the orchestrator returns (`internal/floor/floor.go`, `floor.Apply`):
- **SCA** (`internal/sca/`) — parses lockfiles → OSV.dev queries → findings with `Source: "sca"`.
- **Poison** (`internal/poison/`) — scans only files that enter an LLM context window (`.mcp.json`, manifests, `.md` under `skills/`/`commands/`/`prompts/`) for injection payloads → `Source: "poison"`.

Floor findings carry a non-`"llm"` `Source`, which is exactly how the validator knows to **exempt** them from the verbatim-quote re-read.

### Web UI (`web/`, embedded into the binary by `make build`)

React 19 + Vite 6 + Tailwind v4 + TanStack Router (file routes in `web/src/routes/`) + TanStack Query; pages in `web/src/pages/`. Two big-picture rules the files don't advertise:

- **Design tokens, never raw Tailwind shades.** The palette is oklch CSS variables in `@theme {}` in `web/src/styles/globals.css` (light is the default) with an `html.dark {}` override; `web/src/lib/theme.ts` toggles the class on `documentElement` (localStorage key `assay-theme`). Components consume `var(--color-*)` (e.g. `text-[color:var(--color-danger)]`) — a hardcoded `text-red-400` won't theme and breaks dark mode.
- **Cross-route state lives at the AppShell root, not in the page.** A page component unmounts on navigation, so live state that must persist (an in-flight scan's SSE subscription, the assistant chat transcript) is held in a root context provider in `web/src/components/layout/AppShell.tsx` (`ScanProgressProvider`, `AssistantConversationProvider`) and mirrored to localStorage. Put new cross-route state there too.

Dev loop: `cd web && pnpm dev` runs Vite and proxies `/api` + `/healthz` to a separately-running `assay serve` (`127.0.0.1:7373`); or rebuild the embedded SPA with `make build` and run `./bin/assay serve`.

## Non-negotiable disciplines (enforced in code + review)

These are why Assay is trusted; violating them is a P0. See `CONTRIBUTING.md`.

- **Verbatim-quote rule.** Every LLM finding must cite `file:line` + a verbatim quoted snippet. The post-validator (`internal/verdict/validator.go`) physically re-reads the cited file after Stage 3; if the quote isn't within ±3 lines (whitespace-normalized), the evidence is dropped, and a finding with no surviving evidence is dropped entirely (recorded as `DroppedFinding` in `investigation.log`). Confabulated findings — LLM "remembering" code not in the artifact — are the worst failure mode. If you touch the finding pipeline, keep quoted evidence surviving end-to-end.
- **Bounded tools.** Every agent-callable tool (`internal/tools/`, and the `assay_*` MCP tools) must never escape the scan root. The guard is `FS.resolve` in `internal/tools/fs.go` (reject absolute after `filepath.Clean`; require `HasPrefix(abs+sep, root+sep)`). If you add a tool, add a path-escape test alongside it.
- **No silent failures.** Wrap errors with `fmt.Errorf("doing X: %w", err)`; use sentinel errors + `errors.Is` (never string-compare errors). A swallowed error in the scan pipeline can mean a missed finding.
- **Policy loads from the caller's side, never the target.** `.assay-policy.json` is resolved from the scanning user's cwd or `--policy`, never from inside the scanned artifact — a malicious plugin must not suppress its own findings.

## Prompts

Prompts are the IP and live in `internal/prompts/v1/` (one per stage: `triage.md`, `claims.md`, `threat_model.md`, `investigator.md`, `synthesis.md`), embedded at compile time and selected by `const Version = "v1"`. The MCP-mode equivalent is `internal/mcp/methodology.md`.

Pre-`v0.1.0`: editing `v1/` in place is fine. After `v0.1.0`: non-backward-compatible prompt changes go in a **new directory** (`v2/`, …) so existing reports stay reproducible — bump the version, don't edit in place. If a prompt change measurably affects recall/false-positive rate, include eval numbers against the golden corpus.

## Conventions

- **Spec-first for anything touching the scan pipeline, prompts, or agent loop** (spec → numbered plan → TDD commits). Small bugfixes can skip the spec.
- **Exit codes matter for CI.** `main.go` uses `exitCodeError` so a triggered `--fail-on` gate exits **2** (found problems) vs **1** (tool crashed). `fail_on` precedence: `--fail-on` flag > `policy.fail_on` > default `unsafe`.
- **`internal/` is deliberate** — Assay is a tool, not a library. Programmatic consumers should parse `audit.json` (versioned; `schemas/verdict-v0.1.json`, `schema_version` const `"0.1"`) or the SARIF export (`verdict.ToSARIF`).
- **Storage:** config in `~/.config/assay/config.toml` (XDG-aware; API key is NOT here — OS keychain only); data in `~/.assay/` (scans, content-addressed `cache/`, `fleet/`). All dirs `0750`, files `0600`. Path/atomic-write helpers centralized in `internal/store/`.
- **Auth resolution order** (`internal/auth/`): `ANTHROPIC_API_KEY` env → assay keychain entry → Claude Code OAuth token. `assay auth status` shows which resolved.
