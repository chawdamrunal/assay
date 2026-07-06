---
name: assay-status
description: Show which Anthropic credential source Assay is using.
---

# /assay-status

Display the active authentication method.

## Usage

`/assay-status`

## What this command does

Runs `assay auth status`. Assay resolves credentials in priority order:

1. `ANTHROPIC_API_KEY` environment variable
2. `assay config set api-key sk-ant-...` (stored in OS keychain)
3. Claude Code OAuth credentials (auto-detected from the Claude Code keychain entry)

The output shows which method is currently active, the subscription type (if OAuth), and the token expiry.

Execute:
```bash
assay auth status
```
