---
name: assay-scan
description: Run a full Assay security scan against a target. Drives the 5-stage citation-verified methodology end-to-end via the assay MCP server. No separate Anthropic API call — uses your Claude Code subscription quota.
---

# /assay-scan

Run a citation-verified 5-stage security scan against the given target by following the embedded Assay methodology via the assay MCP server.

## Usage

`/assay-scan <target>`

Where `<target>` is:
- An absolute path to a local directory (e.g., `~/.claude/plugins/my-plugin`)
- A path under the workspace

## What happens

1. Loads the `assay_methodology` prompt from the `assay` MCP server (registered via `.mcp.json` in this plugin).
2. The prompt instructs you to walk the 5 stages — triage, claims, threat model (before reading code), investigation, exploitability + synthesis — calling the `assay_*` MCP tools at each step.
3. At Step 0, calls `assay_scan_start` to allocate `~/.assay/scans/<basename>/<scan_id>/`.
4. At Steps 1-4, calls `assay_list_files`, `assay_read_file`, `assay_grep`, `assay_parse_manifest`, `assay_secret_scan`, and `assay_osv_lookup` to investigate.
5. Every confirmed finding is appended via `assay_record_finding` with a verbatim `file:line` snippet.
6. At Step 6, `assay_finalize_scan` re-reads every cited file:line, drops fabricated evidence, recomputes the verdict from what survives, and writes `audit.json` + `audit.md`.

## Prerequisites

The `assay` binary must be on PATH and the `.mcp.json` in this plugin's directory must be discovered by Claude Code. Install via:

```bash
curl -sSL https://github.com/chawdamrunal/assay/install | sh
```

## Output

After `assay_finalize_scan` returns, you should see:
- The verdict (`safe` / `caution` / `unsafe`)
- The count of surviving findings by severity
- The path to `audit.md` + `audit.json`
- A suggestion to open `assay serve` (http://localhost:7373) to browse the report

## Execute

Load the methodology prompt for the supplied target and follow every step in the order it specifies. The target argument is `$ARGUMENTS`:

```
@assay assay_methodology target="$ARGUMENTS"
```
