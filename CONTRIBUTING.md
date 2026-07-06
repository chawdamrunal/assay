# Contributing to Assay

Assay is a security scanner. False positives waste analyst time; false negatives miss real threats. We welcome contributions, but rigor matters more than velocity. Please read this document before opening a PR.

## Development setup

Requirements:

- Go 1.25+
- Node 22+
- pnpm 10+

Common commands:

- `make build` — builds the Go binary and the web bundle
- `make test` — runs the full suite (Go + TypeScript + integration)
- `make lint` — runs `golangci-lint run` and `pnpm lint`

If `make test` fails on a clean checkout, that's a bug — please open an issue.

## The disciplines we enforce

These are non-negotiable. They exist because Assay's value depends on them.

### TDD

Every new feature lands with a failing test first, then the code that makes it pass. The repo's commit history follows this pattern across 90+ commits — if you're unsure what "good" looks like, run `git log --oneline` and read a few feature commits.

A PR that adds behavior without a test will be sent back.

### Verbatim-quote rule

Every finding Assay produces must cite a `file:line` location plus a verbatim quoted snippet of the offending source. This is enforced at runtime by the post-validator (`internal/verdict`) and again in code review. If your change touches the finding pipeline, make sure quoted evidence still survives end-to-end.

Confabulated findings — anything where the LLM "remembers" code that isn't actually in the artifact — are the single worst failure mode for this tool. We treat regressions here as P0.

### Bounded tools

Agent-callable tools (`internal/tools/`) must never escape the scan root. If you add a tool, add a path-escape test alongside it. The pattern: construct a path that tries to traverse out of the root, assert the tool refuses.

### No silent failures

- Wrap errors with `fmt.Errorf("doing X: %w", err)` so the chain stays intact.
- Use sentinel errors (`var ErrFoo = errors.New(...)`) and branch on them with `errors.Is`. Don't compare error strings.
- A swallowed error in the scan pipeline can mean a missed finding. Don't swallow.

### Lint clean

`make lint` must pass before you push. Both `golangci-lint run` and `pnpm lint` are wired in. CI will reject anything that doesn't lint.

## The flow

We use a spec-first workflow:

1. **Spec** — a short doc describing the problem, the proposed change, and what success looks like.
2. **Plan** — a numbered task breakdown.
3. **Tasks** — each task lands as a TDD-driven commit (test first, then code).

For small bugfixes you can skip the spec, but anything touching the scan pipeline, the prompt set, or the agent loop should have one.

PR descriptions should include:

- What changed
- Why
- Which tests cover it
- Any cost implications (extra API tokens per scan, new external calls, etc.)

## Prompt changes

Prompts live in `internal/prompts/<version>/`. While we're pre-`v0.1.0`, editing the `v1/` prompts in place is fine. After `v0.1.0`, non-backward-compatible prompt changes go into a new directory (`v2/`, `v3/`, …) so existing scan reports remain reproducible.

If your prompt change measurably affects recall or false-positive rate, include the eval numbers in the PR.

## The bar

Before you open a PR, ask: does this change make Assay better at finding real threats with verifiable evidence, without confabulating?

If the answer isn't a clear yes, expect pushback. We'd rather ship slower than ship a scanner that lies.

## Out of scope

See `ARCHITECTURE.md`'s "What we deliberately don't do (yet)" section. Ring 1, Ring 2, and Ring 3 features (the boundary is documented there) need a spec doc and a design discussion before implementation work starts. Opening a 2000-line PR for a Ring 2 feature without prior discussion is the fastest way to get it closed.

## Reporting bugs

- **Functional bugs in Assay** — open a GitHub issue with a minimal repro (a small testdata artifact is ideal).
- **Security bugs in Assay itself** — see `SECURITY.md`. Do not open a public issue.

## Code of conduct

By participating, you agree to abide by `CODE_OF_CONDUCT.md`.
