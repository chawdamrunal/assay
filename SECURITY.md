# Security Policy

This document covers vulnerabilities **in Assay itself**. Vulnerabilities Assay finds in the artifacts it scans belong in audit reports, not here.

## Reporting a vulnerability

Report a vulnerability privately through the repository's **Security → Report a vulnerability** tab on GitHub (GitHub Security Advisories). Please don't open a public issue for a security report.

Please do not open a public GitHub issue for security bugs. Include:

- A description of the issue
- Steps to reproduce (a minimal testdata artifact is ideal)
- The version of Assay affected (`assay version`)
- Your assessment of impact, if you have one

PGP key: TBD (will be published before `v0.1.0`).

## What we'll do

- **Acknowledge** within 72 hours.
- **Triage** and confirm or reject the report, usually within a week.
- **Patch** in a private branch, then coordinate a release.
- **Credit** the reporter in `CHANGELOG.md` and the release notes, if they want credit. We're happy to honor anonymity requests.

We aim to disclose publicly within 90 days of the initial report, or sooner once a fix is shipped.

## Supported versions

While we're in `0.x`, only the **latest minor release** receives security fixes. After `v1.0.0`, we'll commit to a longer support window (TBD in the release notes for `v1.0.0`).

| Version  | Supported           |
| -------- | ------------------- |
| latest   | yes                 |
| anything older | no            |

## Hardening notes

Assay is designed to run on an analyst's machine against untrusted artifacts. The following defenses are in place:

- **File permissions** — output directories are created with `0750`, files with `0600`. Reports may contain sensitive findings; we don't want them world-readable.
- **API keys** — default MCP mode uses **no API key**: it reaches the model by spawning `claude -p`, which authenticates through your existing Claude Code login. Only `--scan-mode legacy` uses an Anthropic API key, read from the OS keychain (`security` on macOS, `secret-tool` on Linux); an environment-variable fallback exists but logs a warning.
- **Bounded tools** — every agent-callable tool refuses paths outside the scan root. Symlinks pointing out of the root are rejected.
- **Network egress** — Assay's own outbound traffic is limited to `api.osv.dev` (vulnerability database), plus `api.anthropic.com` only in `--scan-mode legacy`. In default MCP mode the model call is made by the `claude -p` subprocess, whose egress goes through Claude Code — not a direct Assay connection.
- **`--offline` flag** — disables OSV lookups entirely. Useful for air-gapped environments and for scanning artifacts you'd rather not advertise to a third party.

## Threat model of Assay itself

Assay is a **local tool**. It does not run a network service, it does not ingest untrusted input from the network, and it does not execute the artifacts it scans — it only reads them.

The trust boundary is:

- The user's local machine (trusted)
- The Anthropic API (trusted; messages contain code excerpts from the scanned artifact)
- The scanned artifact (untrusted; treated as inert data)

A malicious artifact cannot execute code by being scanned. The worst it can do is try to exhaust resources (large files, deeply nested structures) — we cap file sizes and recursion depth to mitigate this.

If you find a way to escape any of these boundaries — for example, getting Assay to execute attacker-controlled code, read files outside the scan root, or exfiltrate data to an attacker-controlled host — please report it via the email above.
