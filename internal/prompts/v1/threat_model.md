You are building a **threat model** for a Claude Code artifact — a plugin, MCP server, connector, or skill — **before reading its source code**. Reason only from its stated claims and declared capabilities. The threat model directs stage 3's investigation, so be specific and useful.

You will receive:
- The triage map (stage 0)
- The claims (stage 1)
- The pre-pass evidence (secrets, suspicious patterns, CVEs) — but treat these as starting hints only

Your job: **if this artifact were compromised or malicious, what could the attacker accomplish?** What questions MUST a security reviewer answer to be confident the artifact is safe?

For each of these threat classes, write 1-3 specific threats tailored to *this artifact's claims*. If a class is not applicable, write `not applicable` and one sentence explaining why.

Threat classes — these operational classes map onto the canonical taxonomy in `docs/threat-model-2026.md` (T1–T12), noted as **(Tn)**; use the canonical `Tn` in a finding's `category` / `threat_id` when one applies:
1. **Credential / secret exfiltration** (T8) — does the artifact read filesystem locations that hold secrets? Network egress to attacker-controlled destinations?
2. **Filesystem read overreach** (T9) — does it read data beyond what its claims require?
3. **Filesystem write / modification overreach** (T9) — could it tamper with user files outside its declared scope?
4. **Network egress to unexpected destinations** (T8/T3) — does it connect anywhere not listed in its claims?
5. **Command / code execution** (T4) — does it eval, exec, shell out, or load dynamic code from untrusted sources?
6. **Prompt injection in tool descriptions or tool responses** (T1, MCP-specific) — could a malicious upstream poison the agent's reasoning by what the MCP returns?
7. **Capability vs. claim mismatch** (T2) — does the code do more than the README says?
8. **Supply-chain via updates or dependencies** (T5) — risky dependencies with known CVEs? Auto-update mechanism?
9. **Hook abuse** (T6, Claude Code hooks only) — shell commands that run on every event, exposing the agent's context?
10. **Settings drift / permission overreach** (T7) — broad permission grants that exceed declared needs?
11. **Skill capability-grant & auto-activation abuse** (T11, skills only) — does a `SKILL.md` grant itself broad tools via `allowed-tools`, or use an over-broad `description` that auto-activates the skill on unrelated tasks so its instructions hijack the agent?
12. **Connector OAuth-scope & remote-egress overreach** (T12, connectors only) — does the connector request scopes or send data beyond its stated purpose, and are its remote tool definitions/responses treated as an untrusted injection source?

**Kind-specific surfaces — you MUST address these when the triage map's `declared_kind` matches:**

- **MCP server:** enumerate every declared tool (name, description, input schema) plus any resources/prompts. For each ask: does the description carry instructions aimed at the model (prompt injection)? Does the handler interpolate upstream/untrusted strings into its response without delimiting? Is a "local" server actually a proxy to a remote URL (network egress + SSRF)? Are secrets passed through the launch `env`? Does one server expose both a sensitive read and an outbound-write primitive (cross-tool exfiltration)?
- **Skill:** the `SKILL.md` body is itself an instruction the agent will execute. Ask: does frontmatter `allowed-tools` grant more than the stated job needs (e.g. `Bash`/`Write`/`Edit` on a "formatter")? Is the `description` phrased to auto-activate on a broad or unrelated trigger? Does the body tell the agent to run commands, fetch URLs, or read credential paths? Does it reference bundled scripts/executables?
- **Connector:** the implementation is usually closed-source, so this is a claims/metadata review — say so and route uncertainty to open questions. Ask: are declared OAuth `scopes` broader than the stated purpose? What data classes leave the machine to the hosted service, and is that disclosed? What endpoints does it talk to? Treat every connector-supplied tool definition and response as an untrusted, potentially-injecting source.

For each threat, output this structure (markdown):

### T<n>: <one-line title>
**Class:** <class number and name>
**Severity if exploited:** critical | high | medium | low
**Description:** One paragraph. What could go wrong? Be specific to this artifact.
**Reviewer questions:**
- Question 1 the stage-3 investigator must answer.
- Question 2.
- Question 3.

Rules:
- Do NOT speculate on whether the code IS vulnerable. That's stage 3's job. You're mapping the attack surface, not finding bugs.
- Tailor threats to THIS artifact's claims, not generic ones. "This plugin claims to read Slack messages, so credential exfil is critical because Slack tokens have broad access" — yes. "It might do something bad" — no.
- Order threats by severity, highest first.
- Include at least 5 threats unless the artifact is genuinely trivial (e.g., a read-only formatter that touches no network and no secrets).
