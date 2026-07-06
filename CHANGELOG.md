# Changelog

All notable changes to Assay are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Security (v0.5.1 — hardening sprint)

- **CSRF middleware on every mutation route** — `POST /api/scans`, `POST /api/fleet/scan`, `POST /api/assistant/message`, `PUT /api/config`, and `DELETE /api/scans/:id` now require an `X-Assay-CSRF: 1` header. Browsers refuse to set custom headers on cross-origin simple requests without a preflight, so a malicious page the user has open in another tab can no longer trigger scans or delete state on the localhost server. New `internal/api/csrf.go` middleware + 4-case test in `internal/api/csrf_test.go`. FE `jsonFetch` in `web/src/lib/api.ts` sends the header on every request.
- **Path-traversal guard on `POST /api/scans` and the chat-assistant scan path** — the `target` field is now validated against a whitelist of allowed roots (default: `~/.claude/plugins/`, OS temp dir, current working directory). Symlink escape, `../` traversal, sibling-prefix collisions all rejected with 403. Without this guard a CSRF (or naive misuse) could scan `/etc/passwd`. New `internal/api/pathguard.go` + 9-case test.
- **`ScanRunner.Emit`/`Complete` race fix** — Emit released the runner mutex before sending on the per-scan channel, racing with Complete which closed that channel. The `select { default: }` branch does NOT save you from sending on a closed channel — that always panics. Now a per-scan mutex serialises the closed-check and send. Tested with a 200-iteration emit/complete race fuzzer under `go test -race`.
- **Fleet `GET /api/fleet/:id` 404 fix** — `errors.Is(err, fmt.Errorf("not found"))` always returned false because `fmt.Errorf` allocates a fresh error every call. Every missing-fleet request returned HTTP 500. Now `internal/fleet.ErrFleetNotFound` is a stable sentinel and the handler returns 404 correctly.

### Changed (v0.5.1)

- **`assay scan --output PATH`** — implemented (was previously a silent no-op). After a scan completes, audit.json + audit.md are copied to PATH in addition to the canonical history. Useful for CI workspaces and shared report dirs.
- **`assay scan --no-cache`** — hidden from `--help` until the SBOM/attestation cache lands in v0.6 (was previously a silent no-op).
- **`assay scan-all --quick`** — hidden from `--help` until the runner integration lands in v0.6 Phase B. When set, the CLI now emits a clear warning rather than silently falling back to full scans.
- **`assay serve --scan-mode mcp` honors `offline`** — the `offline` field from `POST /api/scans` was previously discarded at the MCP spawn boundary. Now threaded through `SpawnConfig.Offline` and rendered into the methodology prompt as an explicit "do NOT call `assay_osv_lookup`" directive.

### Added (v0.5 — chat assistant)

- **`Ask Assay` chat surface** — new `/assistant` page where users ask "is vercel safe?" and Assay resolves candidates from inventory + marketplace cache, proposes them as cards, and embeds a live ChatScanThread when the user confirms. New `internal/assistant/` package (intent + resolver + conversation store) with 18 test cases. `POST /api/assistant/message` returns a discriminated reply (text / proposal / scan_started / error).
- **`@excalidraw/excalidraw` flow diagrams with native zoom + pan** — the Data Flow section on the scan report now renders the Mermaid diagram into an Excalidraw canvas via `@excalidraw/mermaid-to-excalidraw`. Includes a colour legend strip (external net / sensitive sink / local FS / trust boundary), Fit + Expand toolbar buttons, and an ESC-to-exit fullscreen mode. Methodology prompt updated to cap the diagram at 10 nodes with mandatory grouping so dense plugins stay legible.
- **`ChatScanThread` on `/scans/live/:id`** — the live-scan page is now a conversation: each SSE stage event becomes an Assay chat bubble with typewriter reveal, status pill, and a "thinking…" bubble between stages. Replaces the dense `[stage] start — message` log with something the user can read at a glance. New `web/src/components/ChatScanThread.tsx`.

### Added (v0.4 — answers the questions `git clone` can't)

- **`assay scan-all` + Fleet dashboard** — scan every installed Claude Code plugin in parallel, aggregate the verdicts into one report. New CLI command (`bin/assay scan-all [--parallel N] [--exclude foo,bar]`), HTTP routes (`POST /api/fleet/scan`, `GET /api/fleet`, `GET /api/fleet/:id`, `GET /api/fleet/:id/stream`), and Fleet pages in the web UI (`/fleet`, `/fleet/:id`). Demonstrably impossible to do by hand at scale. Backed by `internal/fleet/` (8 tests).
- **Diff-mode scanning** — when scanning a target with `since: "latest"` (or `since: "<scan_id>"`), Assay auto-diffs new findings against the prior scan and annotates each as `new` / `stable` / `changed` / `resolved`. New `GET /api/scans/diff?a=...&b=...` endpoint, new `/scans/diff` page (side-by-side), per-finding diff chip on `FindingCard`, "Compare to previous" link on the report page. `verdict.Diff` helper + 7 tests; diff endpoint with 4 tests. Works in both MCP mode and fake mode.
- **Pre-install gate hook** — `assay hook install` writes a `UserPromptSubmit` hook into `~/.claude/settings.json` that fires `assay scan --quick` on `/plugin install <ref>` and returns a `permissionDecision`: critical/high → deny, medium → ask, low → allow with `additionalContext`. Includes deep-scan fork so the full verdict completes in the background. Idempotent install / status / uninstall round-trip with 7 tests. The slash-command intercept point (vs PreToolUse, which can't catch slash commands) was confirmed via research of plugin-dev's hook-development SKILL.md.
- **`assay scan --quick`** — tier-1 deterministic scan profile (no LLM call). Runs pre-pass + risk heuristic in <2s. New `scanner.RunQuick` + 5 tests. Returns `{risk, counts, deep_scan_id}` as compact JSON for shell consumption.
- **`assay hook resolve <ref>`** — resolves `<name>@<marketplace>` (or just `<name>`) to its on-disk source under `~/.claude/plugins/marketplaces/...` (or `cache/...` fallback). Used by the gate script.
- **`target.Hash` populated in MCP + fake modes** — was previously only set on the CLI scan path. Now reliably populated via `inventory.HashDir(target)` before audit.json is written; enables content-aware diff matching across two installs of the same plugin version.
- **New `sensitive-path-constructed` pre-pass pattern** — catches `path.join(os.homedir(), '.aws', ...)`/`os.homedir() + '/.ssh/'` style constructed paths that the inlined-path regex missed. Promotes "cosmetic plugin that touches .aws/" from low to high risk in QuickProfile.
- **Frontend additions** — `web/src/types/api.ts` extended with `DiffAnnotation`, `Finding.diff`, `Verdict.prior_scan_id`, fleet types. `web/src/lib/api.ts` gains `startFleetScan`, `getFleet`, `listFleets`, `openFleetStream`, `getDiff`. New pages: `FleetListPage`, `FleetDetailPage`, `ScanDiffPage`. Sidebar "Fleet" entry. New "Compare against latest prior scan" checkbox on `NewScanPage`.

### Added (v0.3)

- **`assay mcp` subcommand** — Assay now exposes itself as a Model Context Protocol server (stdio or HTTP transport). Ten tools published: `assay_list_files`, `assay_read_file`, `assay_grep`, `assay_parse_manifest`, `assay_osv_lookup`, `assay_secret_scan`, `assay_scan_start`, `assay_emit_progress`, `assay_record_finding`, `assay_finalize_scan`. Plus the `assay_methodology` prompt — the 5-stage playbook with target substitution.
- **MCP-driven `assay serve` default mode** — `assay serve` now defaults to `--scan-mode mcp`: clicking "New Scan" in the web UI spawns `claude -p` as a subprocess with the assay MCP wired in, runs the scan under your Claude Code subscription quota, tails `events.jsonl` for live SSE progress, and produces the same `audit.json` + `audit.md` the legacy orchestrator does. No 429 problems because Claude Code manages the subscription rate-limit itself.
- **`/assay-scan` Claude Code slash command** — load the embedded methodology prompt and drive a scan from inside Claude Code with no Anthropic API call from Assay itself.
- **`internal/claude/retry.go` — automatic 429 + 5xx retry with exponential backoff** for the legacy in-process orchestrator. Honors `retry-after` / `anthropic-ratelimit-*-reset` headers when the server sends them. Wired into both `assay scan` (CLI) and `assay serve --scan-mode legacy`. Surfaces "rate-limited (attempt N) — retrying in Xs" events to the SSE stream so the UI is never silent.
- **`assay serve --scan-mode fake`** — replay recorded fixtures from `testdata/recorded/` for offline development and demos. Zero LLM calls.
- **Dashboard counters wired to live data** — Plugins / MCP servers / Completed-scans tiles populated from `/api/inventory` + `/api/scans` instead of hardcoded em-dashes.
- **History date fallback** — UUID-IDed scans now bucket under Today/Yesterday by reading the directory mtime via the new `created_at` field returned by `/api/scans`, instead of falling into the "Unknown" bucket.
- **Failed-scan UX** — a scan that fails mid-flight now persists `error.json` to its scan dir; `GET /api/scans/:id` returns HTTP 410 Gone with the structured failure; the `/scans/:id` page renders a dedicated "Scan failed" card with stage, target, and timestamp instead of a generic 404.
- **`Pending` / `Failed` / `Complete` badges** in the Scans List, driven by per-scan `status` in the list payload.

### Fixed

- Phantom empty scan directories — the in-process orchestrator no longer leaves a scan dir with no audit and no error file.
- Live Scan page no longer swallows the actual SSE failure reason — it now polls `/api/scans/:id` on stream error and surfaces the real message.

### Architecture

- `internal/mcp/` (new) — MCP server + tools + methodology prompt + event-tailer + Claude-Code subprocess spawner. 12 tests pass in-process via mcp-go's InProcessClient.
- `internal/verdict/Validate` and `internal/verdict/RenderMarkdown` now feed both orchestrators identically — citation post-validator runs whether the scan was driven by Claude Code (MCP) or the Go orchestrator (legacy).

## [0.1.0] - 2026-05-15

Initial public release. The first security scanner for the AI dev stack — Sonnet-driven threat modeling for Claude Code plugins, MCP servers, and connectors.

### Added

#### Scanner engine
- Five-stage agent loop: triage → claim extraction → threat model → parallel investigation → exploitability → synthesis
- Threat model produced BEFORE reading source code — reasons from claims + declared capabilities to attack surface
- Parallel investigator sub-agents (one per threat, concurrency-controlled)
- Verbatim-quote enforcement: every finding cites a `file:line` + verbatim snippet, post-validated by re-reading the file
- Deterministic pre-pass: secret scanning (6 named rules), suspicious-pattern detection (11 rules), OSV CVE lookup
- Tool layer: `read_file`, `list_dir`, `grep`, `parse_manifest`, `record_finding`, `osv_lookup`, `secret_scan`, `dispatch_subagent` — all bounded under the scan root
- Per-scan budget cap with graceful stop (partial findings + open-question note)
- Prompt-cache support across stages (system, tool defs, triage map)

#### CLI
- `assay scan <target>` — full scan against a path or inventory name
- `assay inventory` — enumerate Claude Code plugins, MCP servers, hooks, and settings overrides on the local machine
- `assay config get|set|list` — manage configuration (TOML at `~/.config/assay/config.toml`)
- `assay serve` — launch local web UI on `localhost:7373`
- `assay auth status` — show active credential source
- `assay version` — version + build metadata

#### Web UI
- Premium dark-mode-first React 19 + Vite + Tailwind v4 + shadcn-style components
- Inventory page consumes live `/api/inventory`
- Scan Reports list page (`/scans`) backed by `/api/scans`
- New Scan page (`/scan/new`) triggers POST `/api/scans` with target picker
- Live Scan progress page (`/scans/live/:id`) consumes SSE stream
- Scan Report detail page (`/scans/:id`) — the showcase, with Mermaid threat-model diagrams, Shiki-highlighted code findings, and a severity-sorted, filterable finding list
- History timeline grouped by date
- Theme toggle (dark/light)
- Embedded into the Go binary via `embed.FS` — single static binary, no separate frontend deploy

#### Authentication
- Four-method credential resolution in priority order: `ANTHROPIC_API_KEY` env → assay-managed OS keychain → Claude Code OAuth (auto-detected)
- Bearer-token support for OAuth flow (Claude Code subscribers don't need a separate API key)
- `assay auth status` shows which method is active and the subscription type for OAuth

#### Verdict + audit artifacts
- Public JSON Schema at `schemas/verdict-v0.1.json`
- Per-scan artifacts: `audit.md` (human-readable), `audit.json` (machine-readable), `investigation.log` (full agent trace), `evidence/` (preserved snippets)
- Deterministic markdown renderer as a fallback for the LLM synthesis output
- Citation post-validator drops findings whose snippets don't appear in the cited file

#### Distribution
- Multi-stage Dockerfile producing ~10MB scratch-based image
- `install.sh` POSIX-shell installer (curl | sh) with checksum verification
- `goreleaser` config for multi-arch (darwin/arm64, darwin/amd64, linux/amd64, linux/arm64, windows/amd64) binaries, Docker images, and Homebrew formula
- GitHub Actions release workflow triggered on `v*.*.*` tag push

#### Claude Code plugin
- `plugin/` directory ready for marketplace submission
- Slash commands: `/assay-scan`, `/assay-inventory`, `/assay-status`
- Pre-install advisor skill that activates on plugin/MCP install mentions

#### Documentation
- README, ARCHITECTURE, CONTRIBUTING, SECURITY, CODE_OF_CONDUCT
- `docs/threat-model-2026.md` — the launch document; defensive taxonomy of 12 threat classes in the Claude Code + MCP ecosystem
- Design overview in [ARCHITECTURE.md](ARCHITECTURE.md)

#### Testing
- Default suite: free, FakeClient-based, runs on every PR
- Integration suite (`-tags integration`): replay recorded Sonnet responses against the golden corpus (5 safe + 5 vulnerable fixtures)
- Smoke suite (`-tags smoke`): hits the real Anthropic API, requires `ANTHROPIC_API_KEY`, ~$0.20 per run, excluded from CI

### Known limitations

These are deferred to future releases (see [ARCHITECTURE.md](ARCHITECTURE.md)):

- No dynamic execution sandbox — Assay is a static analyzer (Ring 2)
- No adversarial prompt-injection probing (Ring 2)
- No cross-client adapters (Cursor, Cline, Continue) — Claude Code + MCP only (Ring 1)
- claude.ai connector scanning is metadata-only — closed-source connectors can't be deep-reviewed (Ring 1)
- No real-time MCP firewall — detection only, not prevention (Ring 3)
- Verdicts are not yet community-shared (no public verdict database) (Ring 1)
- Smoke test covers one fixture (rainbow-formatter); broader real-API regression coverage as budget allows
- The web UI's Dashboard page shows placeholder aggregate counts; wiring up real numbers is a follow-up

### Security

- All credentials live in the OS keychain
- Source code stays on the user's machine; only file snippets the agent chooses to read are sent to the Anthropic API
- Tools are bounded under the scan root — agents cannot read `~/.ssh/` or escape the target directory
- HTTP server binds to `127.0.0.1` only by default
- Citation post-validator is the anti-confabulation firewall: any finding whose snippet doesn't match the file is silently dropped

### Migration notes

N/A — this is the first release.

[Unreleased]: https://github.com/chawdamrunal/assay/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/chawdamrunal/assay/releases/tag/v0.1.0
