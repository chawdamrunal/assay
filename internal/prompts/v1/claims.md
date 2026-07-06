Read the artifact's README and manifest. Write a faithful, paragraph-form description of what this artifact — a plugin, MCP server, connector, or skill — **claims** to do. Do not editorialize, judge, or speculate. Report only what the documentation says.

You will receive:
- The triage map from stage 0
- The full text of README, README.md, or equivalent
- The manifest (already parsed)

Produce a single JSON object:

{
  "claims_paragraph": "Plain prose, 2-4 sentences. Start with 'This plugin/MCP claims to ...'.",
  "declared_capabilities": ["bullet 1", "bullet 2"],
  "declared_permissions": ["read:foo", "write:bar"],
  "declared_network": ["domains or endpoints the artifact says it contacts"],
  "declared_dependencies": ["package names", "external services"],
  "trust_signals": ["author/publisher info", "version", "signing info if any"]
}

Rules:
- Quote-style faithfulness: if the README says "syncs your Slack to Jira," you write that. You don't say "appears to sync."
- If a field has no claim, use an empty array. Don't invent.
- If the README is missing or empty, return all-empty fields and set `claims_paragraph` to "No README or manifest claims available."
- Kind-specific claims: for a **skill**, the `SKILL.md` frontmatter `allowed-tools` is a declared capability (record it in `declared_permissions`) and the `description` declares when it auto-activates (note it in `claims_paragraph`). For an **MCP server**, list each declared tool's stated purpose. For a **connector**, record declared OAuth `scopes`, endpoints, and data classes in `declared_permissions` / `declared_network`.
