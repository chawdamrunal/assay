---
name: assay-inventory
description: List installed Claude Code plugins, MCP servers, hooks, and settings overrides.
---

# /assay-inventory

Enumerate everything on the local machine that Assay can scan.

## Usage

`/assay-inventory`

## What this command does

Runs `assay inventory`. The output is a table with one row per detected item:

- **Claude Code plugins** — directories under `~/.claude/plugins/` with a `plugin.json`
- **MCP servers** — entries under `mcpServers` in `~/.claude/settings.json`
- **Hooks** — every `PreToolUse`, `PostToolUse`, `UserPromptSubmit`, etc.
- **Settings overrides** — `permissions.allow` / `permissions.deny` grants

For each item, the table shows name, kind, version (if declared), and source URL (if declared).

This is the input source for `/assay-scan`: you can pick a target by name from the inventory.

Execute:
```bash
assay inventory
```
