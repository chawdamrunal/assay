# Assay — Claude Code plugin

This plugin wraps the [Assay](https://github.com/chawdamrunal/assay) security scanner for the AI dev stack.

## Slash commands

- `/assay-scan <target>` — run a full 5-stage scan against a plugin, MCP server, or directory
- `/assay-inventory` — list every plugin, MCP server, hook, and settings override on your machine
- `/assay-status` — show which auth method Assay is using (env / assay keychain / Claude Code OAuth auto-detect)

## Skill: pre-install advisor

When you mention installing a new plugin or MCP server, the `assay` skill activates and offers to scan it first. This is opt-in — the skill prompts before invoking the scanner.

## Requirements

The plugin shells out to the `assay` binary. Install it first:

```bash
curl -sSL https://github.com/chawdamrunal/assay/install | sh
# or:
brew install chawdamrunal/tap/assay
```

After install, Assay auto-detects your Claude Code OAuth credentials — no separate API key required.

## Cost

Scans cost roughly $0.10–$0.50 per artifact in Anthropic API tokens. If you're a Claude Code Pro/Max subscriber, this is drawn from your subscription's quota (via the bearer token Assay re-uses from the Claude Code keychain).

## License

Apache-2.0. See [LICENSE](../LICENSE) at the repo root.
