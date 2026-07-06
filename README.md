<p align="center">
  <img src="docs/assets/assay-banner.svg" alt="Assay — security scanner for the AI dev stack" width="820">
</p>

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache--2.0-4A3ED4" alt="License: Apache-2.0"></a>
  <img src="https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go&logoColor=white" alt="Go 1.25+">
  <img src="https://img.shields.io/badge/status-pre--1.0-8A8FA0" alt="Status: pre-1.0">
  <img src="https://img.shields.io/badge/MCP-server-4A3ED4" alt="MCP server">
  <a href="https://github.com/chawdamrunal/assay/stargazers"><img src="https://img.shields.io/github/stars/chawdamrunal/assay?color=4A3ED4" alt="GitHub stars"></a>
</p>

<p align="center">
  <b>Reasons about an artifact with an LLM — not regex — to catch prompt injection, credential exfiltration, and MCP tool poisoning in Claude Code plugins, MCP servers, hooks, skills &amp; connectors.</b><br>
  Runs on your <b>Claude Code subscription</b> — no separate API key required.
</p>

<p align="center">
  <a href="https://chawdamrunal.github.io/assay/"><b>📚 Documentation</b></a>
  &nbsp;·&nbsp; <a href="https://chawdamrunal.github.io/assay/installation.html">Install</a>
  &nbsp;·&nbsp; <a href="https://chawdamrunal.github.io/assay/quickstart.html">Quickstart</a>
  &nbsp;·&nbsp; <a href="https://chawdamrunal.github.io/assay/how-it-works.html">How it works</a>
  &nbsp;·&nbsp; <a href="https://chawdamrunal.github.io/assay/faq.html">FAQ</a>
</p>

<!-- ▶ DEMO — replace this block with your recording (a linked thumbnail, a GIF, or a <video> tag). -->
<p align="center"><em>▶ Demo video coming soon</em></p>

---

## What it does

Before you install a plugin or wire up an MCP server, Assay threat-models what it *could* do, then reads the code for evidence — every finding backed by a verbatim `file:line` quote.

- **Reasons, doesn't pattern-match** — an LLM builds a threat model *before* it reads source, so the review is hypothesis-driven, not regex.
- **Catches AI-native threats** — prompt injection, MCP tool poisoning, credential exfiltration, hook abuse, capability-vs-claim mismatch.
- **No confabulation** — a post-validator re-reads every citation and drops anything the model can't back with real code.
- **Runs on your subscription** — default mode drives Claude Code via `claude -p`; no separate API key, no rate-limit walls.
- **One binary, three roles** — the CLI, the `assay serve` web UI, and the `assay mcp` server Claude Code drives.

Verdict: **safe / caution / unsafe**, written as `audit.json` + `audit.md`.

## Quickstart

```bash
git clone https://github.com/chawdamrunal/assay.git && cd assay
make build && make install          # single binary, React UI embedded

assay inventory                     # what's installed in ~/.claude
assay serve                         # http://localhost:7373 → "New Scan"
```

Full guide → **[Installation](https://chawdamrunal.github.io/assay/installation.html)** · **[Quickstart](https://chawdamrunal.github.io/assay/quickstart.html)**.

## Documentation

The full docs live at **[chawdamrunal.github.io/assay](https://chawdamrunal.github.io/assay/)**:

* **[Installation](https://chawdamrunal.github.io/assay/installation.html)** — build from source, curl / Homebrew / WinGet / Docker, auth
* **[Quickstart](https://chawdamrunal.github.io/assay/quickstart.html)** — scan from the web UI or CLI, the pre-install gate, private GitHub repos
* **[How it works](https://chawdamrunal.github.io/assay/how-it-works.html)** — the 5-stage methodology, both scan modes, threat coverage
* **[FAQ](https://chawdamrunal.github.io/assay/faq.html)** — vs. Snyk / Cisco, API-key needs, tool poisoning, source privacy

Deeper references in-repo: [ARCHITECTURE.md](ARCHITECTURE.md) · [threat model](docs/threat-model-2026.md) · [CHANGELOG.md](CHANGELOG.md).

## Status

Pre-1.0, under active development. The MCP-server architecture is the default scan path; the legacy in-process orchestrator remains as an API-key / CI fallback. Report a security issue in Assay itself via [SECURITY.md](SECURITY.md).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). TDD-disciplined; every finding must cite a verbatim quote — enforced at runtime and in review.

## License

Apache-2.0. See [LICENSE](LICENSE).
