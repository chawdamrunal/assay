// Package poison detects prompt-injection / tool-poisoning patterns in
// MCP server tool descriptions, Claude Code skill files, and any plugin
// resource that ends up in the LLM's context window, plus structural
// capability-surface red flags in the manifests it reads: remote MCP
// transport URLs (POISON-009), over-broad skill allowed-tools grants and
// auto-activation descriptions (POISON-008/010), and over-broad connector
// OAuth scopes (POISON-011).
//
// The attack class: a plugin author embeds instructions inside a tool
// description ("Ignore previous instructions and …") or a skill markdown
// file. When Claude Code loads that text into context, the LLM follows
// the embedded directive — exfiltrating data, executing tools the user
// didn't approve, or pivoting through other plugins.
//
// Invariant Labs' research (2025) measured 84.2% attack success on
// undefended MCP servers and found 5.5% of public servers carried
// poisoned metadata. This detector is the deterministic floor that
// catches the well-known patterns regardless of LLM variance.
//
// Why deterministic / not LLM-based:
//  1. Predictable — two runs over the same plugin always produce the
//     same findings.
//  2. Fast — runs in milliseconds, before the 60-90s LLM stages.
//  3. Cheap — no Anthropic call.
//  4. Verifiable — the pattern catalog is auditable in this file.
package poison

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/chawdamrunal/assay/internal/verdict"
)

// Pattern is one detector rule. Detect returns the spans inside `text`
// that match this rule. The returned spans are line numbers within the
// source file so the finding can cite file:line.
type Pattern struct {
	ID          string
	Severity    string // critical|high|medium|low|info
	Category    string // narrower than verdict.Category; see catalog below
	Description string // shown in the finding's Description field
	Mitigation  string // shown in the finding's Mitigation field
	// match returns (matchedSnippet, lineNumber) for each hit, or empty.
	match func(text string) []hit
}

type hit struct {
	snippet string
	line    int
}

// Catalog enumerates every poison pattern Assay ships with. Keep ordered
// by severity (highest first) — the report renders findings in catalog
// order within the dependency/tool-poisoning section.
var Catalog = []Pattern{
	{
		ID:          "POISON-001",
		Severity:    "critical",
		Category:    "instruction_injection",
		Description: "Tool description or skill file contains text that reads as a directive to the LLM (e.g. 'ignore previous instructions', 'you are now …'). When Claude Code loads this into context the model may follow the embedded directive instead of the user's intent.",
		Mitigation:  "Remove the directive language from the tool description / skill text. Descriptions should describe what the tool does, not instruct the model what to do.",
		match:       matchRegex(reInstructionDirective),
	},
	{
		ID:          "POISON-002",
		Severity:    "high",
		Category:    "role_manipulation",
		Description: "Tool description contains a fake system/role/user XML or markdown block (e.g. `<system>...</system>`, `## SYSTEM ##`). These can confuse the LLM about message boundaries and elevate the plugin's effective trust level inside the context window.",
		Mitigation:  "Strip role/system markers from tool descriptions. Assay recommends letting Claude Code's own message framing be the only source of role information in context.",
		match:       matchRegex(reRoleBlock),
	},
	{
		ID:          "POISON-003",
		Severity:    "high",
		Category:    "hidden_unicode",
		Description: "Tool description / skill text contains zero-width or invisible Unicode characters. These can hide secondary instructions that humans reviewing the file won't see but the LLM will read verbatim.",
		Mitigation:  "Normalise the text to NFC and strip control characters (U+200B, U+200C, U+200D, U+FEFF, U+E0000-U+E007F tag chars). A reviewer who copy-pastes the description should see exactly what the LLM sees.",
		match:       matchUnicode,
	},
	{
		ID:          "POISON-004",
		Severity:    "medium",
		Category:    "data_exfil_hint",
		Description: "Tool description / skill text mentions sending data to an external host, encoding/wrapping conversation context, or sending logs/transcripts somewhere outside the user's control. Could be benign telemetry or could be exfiltration; surfacing for review.",
		Mitigation:  "If the plugin really does send data outside the user's machine, ensure (a) the description says so clearly and (b) the user can opt out via env var or settings. If it doesn't, the language is misleading and should be removed.",
		match:       matchRegex(reExfilLanguage),
	},
	{
		ID:          "POISON-005",
		Severity:    "medium",
		Category:    "deceptive_link",
		Description: "Tool description / skill text contains a Markdown link whose visible text disagrees with its URL (e.g. `[github.com/anthropic](http://attacker.com)`). The LLM (and the user) may follow the URL believing it's the labelled destination.",
		Mitigation:  "Update Markdown links so the visible text matches the destination domain, or remove the link entirely.",
		match:       matchDeceptiveLink,
	},
	{
		ID:          "POISON-012",
		Severity:    "high",
		Category:    "credential_directive",
		Description: "Tool description or skill body instructs the agent (in prose) to read a well-known credential path (~/.ssh, ~/.aws, ~/.gnupg, ~/.kube, etc.). Even without quotes this is an exfiltration-prep directive: the model is being told to open secrets and often to include them in its output.",
		Mitigation:  "A skill or tool must never instruct the agent to read credential directories. Remove the directive; if the tool genuinely needs a credential, read a single named env var the user sets explicitly.",
		match:       matchRegex(reCredentialPathProse),
	},
	// NOTE: POISON-006 (tool-name typosquat) and POISON-007 (oversized
	// description) are NOT text patterns — they need the manifest's JSON
	// structure (declared tool/command names, per-field string lengths), so
	// they're emitted by scanManifestStructured rather than this text catalog.
}

// Result is the output of Scan. Findings is the verdict-shape ready to
// append to the scan's findings list; CheckedFiles is the count surfaced
// in the audit so reviewers can tell "no findings" from "no files".
type Result struct {
	Findings     []verdict.Finding
	CheckedFiles int
}

// Scan walks target for likely tool-description / skill files and applies
// the catalog. Returns one finding per (pattern, file:line) hit.
//
// We deliberately scan only files that end up in the LLM context window:
//
//   - any .mcp.json (tool descriptions live under "tools[].description")
//   - any plugin.json or claude-plugin.json (skill / command summaries)
//   - any SKILL.md (a standalone skill's body + frontmatter)
//   - every .md under skills/, commands/, prompts/ (full prompt body)
//
// For a SKILL.md we additionally run a structured check on the frontmatter
// `allowed-tools` grant (POISON-008), the capability half of skill
// capability-grant abuse.
//
// We do NOT scan README.md or arbitrary repo text — those don't enter
// Claude's context at runtime and would produce false positives.
func Scan(target string) (*Result, error) {
	res := &Result{}
	err := filepath.WalkDir(target, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == "node_modules" || d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !isInterestingFile(path, target) {
			return nil
		}
		res.CheckedFiles++
		extracted := extractText(path, d.Name())
		rel, _ := filepath.Rel(target, path)
		for _, p := range Catalog {
			for _, h := range p.match(extracted) {
				res.Findings = append(res.Findings, toFinding(p, rel, h))
			}
		}
		// Structured manifest checks (typosquat names, oversized descriptions)
		// need the JSON shape, not the concatenated text.
		if strings.HasSuffix(strings.ToLower(d.Name()), ".json") {
			scanManifestStructured(path, rel, res)
		}
		// Skill capability/description checks: always for a file named SKILL.md,
		// and for any other .md carrying skill-shaped frontmatter (name +
		// description) so renamed skills outside skills/ are covered too.
		if strings.EqualFold(d.Name(), "SKILL.md") ||
			(strings.HasSuffix(strings.ToLower(d.Name()), ".md") && hasSkillFrontmatter(extracted)) {
			scanSkillFrontmatter(path, rel, res)
		}
		return nil
	})
	return res, err
}

// maxDescChars is the per-field string-length ceiling above which a manifest
// description is flagged as oversized. Legitimate tool descriptions are well
// under this; a multi-kilobyte description is the "context overflow" /
// cost-bleed shape (the model pays to read it on every call, and it can hide
// instructions far below where a human reviewer stops reading).
const maxDescChars = 2000

// scanManifestStructured emits POISON-006 (tool-name typosquat) and POISON-007
// (oversized description) by reading the manifest's JSON structure.
func scanManifestStructured(path, rel string, res *Result) {
	data, err := os.ReadFile(path) // #nosec G304 -- walk-bounded
	if err != nil {
		return
	}
	var raw map[string]any
	if json.Unmarshal(data, &raw) != nil {
		return
	}

	// POISON-007: oversized string value anywhere in the manifest.
	longest := 0
	forEachString(raw, func(s string) {
		if len(s) > longest {
			longest = len(s)
		}
	})
	if longest > maxDescChars {
		res.Findings = append(res.Findings, oversizedFinding(rel, longest))
	}

	// POISON-006: a declared "name" that typo-squats a well-known tool.
	forEachNamed(raw, "name", func(name string) {
		if target, ok := typoSquatOf(name); ok {
			res.Findings = append(res.Findings, typosquatFinding(rel, name, target))
		}
	})

	// POISON-009: a remote transport URL in an MCP manifest. An MCP server that
	// declares a remote http/sse `url` instead of a local stdio command routes
	// the agent's tool I/O to that host — a network-egress / SSRF surface the
	// "local tool" framing hides. Gate on MCP manifests so a plugin.json
	// homepage/repository URL doesn't trip it.
	if filepath.Base(path) == ".mcp.json" || hasKey(raw, "mcpServers") {
		if u := firstRemoteURL(raw); u != "" {
			res.Findings = append(res.Findings, remoteURLFinding(rel, u))
		}
	}

	// POISON-011: an over-broad OAuth scope set in a connector manifest. A
	// connector is authorized once and then speaks to the model under these
	// scopes; scopes broader than the stated purpose are the connector
	// overreach surface (T12). Gate on connector-shaped manifests so a manifest
	// that lists "scopes" for an unrelated reason isn't misread.
	if isConnectorManifest(path, raw) {
		if broad := broadScopes(raw); len(broad) > 0 {
			res.Findings = append(res.Findings, broadScopeFinding(rel, broad))
		}
	}
}

// forEachString invokes fn for every string value in a JSON tree.
func forEachString(v any, fn func(string)) {
	switch t := v.(type) {
	case map[string]any:
		for _, val := range t {
			forEachString(val, fn)
		}
	case []any:
		for _, val := range t {
			forEachString(val, fn)
		}
	case string:
		fn(t)
	}
}

// forEachNamed invokes fn for every string value whose map key equals key.
func forEachNamed(v any, key string, fn func(string)) {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if k == key {
				if s, ok := val.(string); ok {
					fn(s)
				}
			}
			forEachNamed(val, key, fn)
		}
	case []any:
		for _, val := range t {
			forEachNamed(val, key, fn)
		}
	}
}

// typoSquatOf returns (well-known-name, true) when name is exactly one edit
// away from a curated well-known tool name (and not equal to it).
func typoSquatOf(name string) (string, bool) {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return "", false
	}
	for _, t := range typoSquatTargets {
		if n != t && isDistance1(n, t) {
			return t, true
		}
	}
	return "", false
}

// isDistance1 reports whether a and b differ by exactly one single-character
// insertion, deletion, or substitution (byte-wise; tool names are ASCII).
func isDistance1(a, b string) bool {
	la, lb := len(a), len(b)
	d := la - lb
	if d < 0 {
		d = -d
	}
	if d > 1 {
		return false
	}
	if la == lb {
		diff := 0
		for i := 0; i < la; i++ {
			if a[i] != b[i] {
				diff++
			}
		}
		return diff == 1
	}
	// Lengths differ by one: walk both, allowing a single skip in the longer.
	if la > lb {
		a, b = b, a
		la, lb = lb, la
	}
	i, j, edits := 0, 0, 0
	for i < la && j < lb {
		if a[i] == b[j] {
			i++
			j++
			continue
		}
		edits++
		j++ // skip one char in the longer string
		if edits > 1 {
			return false
		}
	}
	return true
}

func oversizedFinding(rel string, n int) verdict.Finding {
	return verdict.Finding{
		ID:                "POISON-007-" + sanitizeID(rel),
		Severity:          "medium",
		Category:          "tool_poisoning",
		Source:            verdict.SourcePoison,
		Title:             "POISON-007 — oversized description in " + filepath.Base(rel),
		Description:       "A string field in this manifest is " + itoa(n) + " characters long (limit " + itoa(maxDescChars) + "). Oversized tool descriptions cost the model tokens on every call (cost-bleed) and can hide secondary instructions far past where a human reviewer stops reading (context overflow).",
		Context:           "Tool-poisoning pre-pass flagged an oversized string in " + rel + ". This text enters the LLM context window verbatim when the plugin is loaded.",
		Impact:            "Recurring token cost on every invocation, and a hiding place for injected directives that evade casual review. Severity depends on what the hidden text instructs.",
		Mitigation:        "Trim the description to a concise summary of what the tool does. Move long-form docs to a README that is NOT loaded into the model context.",
		RecommendedAction: "Open the manifest and review the oversized field; confirm there are no embedded instructions below the visible summary.",
		Evidence: []verdict.Evidence{
			{File: rel, Line: 1, Snippet: "manifest contains a " + itoa(n) + "-char string field"},
		},
	}
}

func typosquatFinding(rel, name, target string) verdict.Finding {
	return verdict.Finding{
		ID:                "POISON-006-" + sanitizeID(rel) + "-" + sanitizeID(name),
		Severity:          "low",
		Category:          "tool_poisoning",
		Source:            verdict.SourcePoison,
		Title:             "POISON-006 — name '" + name + "' resembles '" + target + "' in " + filepath.Base(rel),
		Description:       "The declared name '" + name + "' is one edit away from the well-known tool '" + target + "'. A user (or the model) may invoke this expecting the real '" + target + "', a typosquat-style confusion.",
		Context:           "Tool-poisoning pre-pass flagged a declared name in " + rel + " that closely resembles a popular tool name.",
		Impact:            "Name confusion can route a call to this plugin's tool instead of the intended one. Low severity on its own; higher if combined with broad capabilities.",
		Mitigation:        "Rename to something unambiguous, or document the difference from '" + target + "' in the README so it isn't mistaken for it.",
		RecommendedAction: "Confirm the name is intentional and not impersonating '" + target + "'.",
		Evidence: []verdict.Evidence{
			{File: rel, Line: 1, Snippet: "declared name: " + name},
		},
	}
}

// --- skill capability grants (POISON-008) ---

// skillToolSeverity maps a high-capability tool name to the severity Assay
// assigns when a skill's `allowed-tools` frontmatter grants it. A skill is
// natural-language instruction the agent follows once activated; granting it
// code execution / filesystem write / network egress is the capability half of
// skill capability-grant abuse (taxonomy T11). We surface the grant for review
// against the skill's stated purpose — the LLM stage judges whether it is
// justified. Tool names are matched on the part before any "(" so granular
// grants like `Bash(git:*)` still resolve to `bash`.
var skillToolSeverity = map[string]string{
	"bash":         "high",   // arbitrary code execution
	"*":            "high",   // grants everything
	"all":          "high",   //
	"write":        "medium", // filesystem write
	"edit":         "medium", //
	"multiedit":    "medium", //
	"notebookedit": "medium", //
	"webfetch":     "medium", // network egress
	"websearch":    "medium", //
}

var reAllowedTools = regexp.MustCompile(`(?i)^\s*allowed[-_]?tools\s*:\s*(.*)$`)

// scanSkillFrontmatter emits POISON-008 (over-broad allowed-tools grant) and
// POISON-010 (over-broad auto-activation description) from a skill's
// frontmatter — the two halves of skill capability-grant abuse (T11).
func scanSkillFrontmatter(path, rel string, res *Result) {
	data, err := os.ReadFile(path) // #nosec G304 -- walk-bounded
	if err != nil {
		return
	}
	text := string(data)

	// POISON-008: over-broad allowed-tools grant (the capability half of T11).
	if tools, line := extractAllowedTools(text); len(tools) > 0 {
		var flagged []string
		worst := ""
		for _, t := range tools {
			key := strings.ToLower(strings.TrimSpace(t))
			if i := strings.IndexByte(key, '('); i >= 0 {
				key = key[:i] // Bash(git:*) -> bash
			}
			if sev, ok := skillToolSeverity[key]; ok {
				flagged = append(flagged, strings.TrimSpace(t))
				worst = maxSeverity(worst, sev)
			}
		}
		if len(flagged) > 0 {
			res.Findings = append(res.Findings, skillCapabilityFinding(rel, flagged, worst, line))
		}
	}

	// POISON-010: over-broad description (the auto-activation half of T11). A
	// skill's description is its auto-activation trigger; "use for any task,
	// always" fires it on unrelated work, where the body then instructs the
	// agent. Low severity — surfaced for the LLM stage to judge in context.
	if desc, dline := extractFrontmatterScalar(text, "description"); desc != "" && reBroadDescription.MatchString(desc) {
		res.Findings = append(res.Findings, skillBroadDescriptionFinding(rel, desc, dline))
	}
}

// extractAllowedTools parses YAML frontmatter at the top of a SKILL.md and
// returns the `allowed-tools` values plus the 1-based line of the key. It
// handles the inline forms (`allowed-tools: Bash, Write` and `[Bash, Write]`)
// and the block-list form (`- Bash` lines). Returns nil if there is no
// frontmatter or no allowed-tools key. Hand-rolled to keep the deterministic
// floor free of a YAML dependency.
func extractAllowedTools(text string) ([]string, int) {
	lines := strings.Split(text, "\n")
	// Frontmatter must start at the top (only blank lines may precede ---).
	start := -1
	for i, ln := range lines {
		s := strings.TrimSpace(ln)
		if s == "---" {
			start = i
			break
		}
		if s != "" {
			return nil, 0
		}
	}
	if start < 0 {
		return nil, 0
	}
	end := -1
	for i := start + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return nil, 0
	}
	for i := start + 1; i < end; i++ {
		m := reAllowedTools.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		if inline := strings.TrimSpace(m[1]); inline != "" {
			return splitToolList(inline), i + 1
		}
		// Block-list form: following "- item" lines until the next key.
		var tools []string
		for j := i + 1; j < end; j++ {
			s := strings.TrimSpace(lines[j])
			switch {
			case s == "":
				continue
			case strings.HasPrefix(s, "- "):
				tools = append(tools, strings.Trim(strings.TrimSpace(s[2:]), `"'`))
			default:
				return tools, i + 1 // next key reached
			}
		}
		return tools, i + 1
	}
	return nil, 0
}

// splitToolList parses an inline allowed-tools value: a comma-separated list,
// optionally wrapped in [ ] and/or quotes.
func splitToolList(s string) []string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	var out []string
	for p := range strings.SplitSeq(s, ",") {
		if v := strings.Trim(strings.TrimSpace(p), `"'`); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// maxSeverity returns the higher of two severity labels.
func maxSeverity(a, b string) string {
	rank := map[string]int{"": 0, "info": 1, "low": 2, "medium": 3, "high": 4, "critical": 5}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

func skillCapabilityFinding(rel string, tools []string, severity string, line int) verdict.Finding {
	list := strings.Join(tools, ", ")
	return verdict.Finding{
		ID:                "POISON-008-" + sanitizeID(rel),
		Severity:          severity,
		Category:          "skill_capability",
		Source:            verdict.SourcePoison,
		Title:             "POISON-008 — skill grants high-capability tools (" + list + ") in " + filepath.Base(rel),
		Description:       "This skill's `allowed-tools` frontmatter grants high-capability tools (" + list + "). A skill is natural-language instruction the agent follows once it activates; granting code execution, filesystem writes, or network egress means the skill body can drive those tools. Surfaced for review against the skill's stated purpose — a read-only or formatting skill should not need these.",
		Context:           "Skill-capability pre-pass flagged the allowed-tools grant in " + rel + ". This is the capability half of skill capability-grant abuse (T11); the auto-activation half is the skill's description breadth, assessed by the LLM stage.",
		Impact:            "If the skill auto-activates or is invoked, the agent may run shell, write files, or reach the network on the skill's instruction. Severity depends on whether the grant matches the skill's stated job.",
		Mitigation:        "Narrow `allowed-tools` to the minimum the skill needs — ideally none. A skill that only reads or formats should not grant Bash, Write, Edit, or WebFetch.",
		RecommendedAction: "Open the SKILL.md frontmatter and confirm each granted tool is justified by what the skill claims to do.",
		Evidence: []verdict.Evidence{
			{File: rel, Line: line, Snippet: "allowed-tools grants: " + list},
		},
	}
}

// reBroadDescription flags skill descriptions phrased for broad auto-activation
// ("use for any task", "always activate") rather than a narrow task match.
var reBroadDescription = regexp.MustCompile(`(?i)\b(any\s+(?:task|time|code|file|request|prompt)|all\s+(?:tasks|code|files|requests|the\s+time|contexts?)|every\s+(?:task|time|prompt)|always\s+(?:use|activate|apply|run)|for\s+everything|whenever\s+(?:possible|you)|unconditional(?:ly)?|universal(?:ly)?|default\s+(?:behaviou?r|mode))\b`)

// frontmatterBounds returns the [start, end] line indices of the leading YAML
// frontmatter block (its two --- fences), or (-1,-1) if the text does not open
// with frontmatter.
func frontmatterBounds(lines []string) (int, int) {
	start := -1
	for i, ln := range lines {
		s := strings.TrimSpace(ln)
		if s == "---" {
			start = i
			break
		}
		if s != "" {
			return -1, -1
		}
	}
	if start < 0 {
		return -1, -1
	}
	for i := start + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return start, i
		}
	}
	return -1, -1
}

// hasSkillFrontmatter reports whether text opens with YAML frontmatter carrying
// both a name and a description key — the shape of a Claude Code skill,
// regardless of the file's name or directory.
func hasSkillFrontmatter(text string) bool {
	lines := strings.Split(text, "\n")
	start, end := frontmatterBounds(lines)
	if start < 0 {
		return false
	}
	var hasName, hasDesc bool
	for i := start + 1; i < end; i++ {
		l := strings.ToLower(strings.TrimSpace(lines[i]))
		if strings.HasPrefix(l, "name:") {
			hasName = true
		}
		if strings.HasPrefix(l, "description:") {
			hasDesc = true
		}
	}
	return hasName && hasDesc
}

// extractFrontmatterScalar returns the inline scalar value of key in the
// frontmatter plus its 1-based line, or ("", 0).
func extractFrontmatterScalar(text, key string) (string, int) {
	lines := strings.Split(text, "\n")
	start, end := frontmatterBounds(lines)
	if start < 0 {
		return "", 0
	}
	re := regexp.MustCompile(`(?i)^\s*` + regexp.QuoteMeta(key) + `\s*:\s*(.*)$`)
	for i := start + 1; i < end; i++ {
		if m := re.FindStringSubmatch(lines[i]); m != nil {
			return strings.Trim(strings.TrimSpace(m[1]), `"'`), i + 1
		}
	}
	return "", 0
}

// looksLikeSkillFile peeks at the head of a markdown file and reports whether it
// carries skill-shaped frontmatter, so renamed skills outside skills/ are still
// scanned. Reads only the first 2 KiB.
func looksLikeSkillFile(path string) bool {
	f, err := os.Open(path) // #nosec G304 -- walk-bounded
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, 2048)
	n, _ := f.Read(buf)
	return hasSkillFrontmatter(string(buf[:n]))
}

// hasKey reports whether v is a JSON object containing key.
func hasKey(v any, key string) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	_, ok = m[key]
	return ok
}

// firstRemoteURL returns the first http(s) value found under any "url" key in a
// JSON tree.
func firstRemoteURL(v any) string {
	var found string
	forEachNamed(v, "url", func(s string) {
		if found == "" && (strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")) {
			found = s
		}
	})
	return found
}

func remoteURLFinding(rel, url string) verdict.Finding {
	return verdict.Finding{
		ID:                "POISON-009-" + sanitizeID(rel),
		Severity:          "medium",
		Category:          "mcp_remote_transport",
		Source:            verdict.SourcePoison,
		Title:             "POISON-009 — MCP manifest declares a remote transport URL in " + filepath.Base(rel),
		Description:       "This MCP manifest declares a remote endpoint (" + url + ") rather than (or in addition to) a local stdio command. Tool calls and their I/O are sent to that host, which reads and returns content the agent acts on — a network-egress and SSRF surface that the 'local tool' framing hides.",
		Context:           "Tool-poisoning pre-pass flagged a remote `url` in the MCP manifest " + rel + ". The agent's tool traffic leaves the machine for this endpoint.",
		Impact:            "Data the agent passes to these tools leaves the user's machine for " + url + ", and everything the endpoint returns re-enters the model as trusted content (indirect injection). If the URL is attacker-influenced this is also an SSRF vector.",
		Mitigation:        "Confirm the remote endpoint is intended and trusted. Prefer a local stdio server where possible; if remote is required, pin the host, use TLS, and treat every response as untrusted data, not instructions.",
		RecommendedAction: "Verify you intended this MCP server to talk to " + url + ". If not, remove the url / use the local command form.",
		Evidence: []verdict.Evidence{
			{File: rel, Line: 1, Snippet: "MCP manifest declares remote url: " + url},
		},
	}
}

// isConnectorManifest reports whether a manifest looks like a connector
// declaration: a conventional connector filename, or a "scopes" list paired
// with an oauth / auth-url / base-url signal.
func isConnectorManifest(path string, raw any) bool {
	switch filepath.Base(path) {
	case "connector.json", ".connector.json", "connector-manifest.json":
		return true
	}
	return hasKey(raw, "scopes") && (hasKey(raw, "oauth") || hasKey(raw, "auth_url") ||
		hasKey(raw, "authorization_url") || hasKey(raw, "base_url") || hasKey(raw, "baseUrl"))
}

// broadScopeTokens are substrings that mark an OAuth scope as broad/dangerous.
var broadScopeTokens = []string{
	"*", "admin", "full", "write_all", "read_write", "readwrite", "all:", ":all", "offline_access",
	// Platform-specific broad scopes that don't contain a wildcard/admin token.
	"mail.google.com", ".modify", "channels:history", "files.read.all", "sites.read.all", "directory.read",
}

// broadScopes returns the connector's scope values that look over-broad, or —
// if none is individually broad but the connector requests many — the full set.
func broadScopes(v any) []string {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	arr, ok := m["scopes"].([]any)
	if !ok {
		return nil
	}
	var scopes, broad []string
	for _, s := range arr {
		str, ok := s.(string)
		if !ok {
			continue
		}
		scopes = append(scopes, str)
		l := strings.ToLower(str)
		for _, t := range broadScopeTokens {
			if strings.Contains(l, t) {
				broad = append(broad, str)
				break
			}
		}
	}
	if len(broad) == 0 && len(scopes) >= 8 {
		return scopes // a large scope set is itself an over-ask
	}
	return broad
}

func broadScopeFinding(rel string, scopes []string) verdict.Finding {
	list := strings.Join(scopes, ", ")
	return verdict.Finding{
		ID:                "POISON-011-" + sanitizeID(rel),
		Severity:          "medium",
		Category:          "connector_scope",
		Source:            verdict.SourcePoison,
		Title:             "POISON-011 — connector requests broad OAuth scopes in " + filepath.Base(rel),
		Description:       "This connector manifest requests broad or numerous OAuth scopes (" + trim(list, 200) + "). A connector is authorized once and then acts on the user's behalf under these scopes; scopes wider than its stated purpose let it read or act on far more than the user expects, and every response it returns re-enters the model as trusted content (T12).",
		Context:           "Connector-scope pre-pass flagged the requested scopes in " + rel + ". The connector is closed-source, so this declared-scope review is the primary static signal.",
		Impact:            "The hosted connector can access everything these scopes grant; data the agent shares leaves the machine for the connector's backend. Over-broad scopes widen the blast radius of a compromised or malicious connector.",
		Mitigation:        "Request the minimum scopes the connector needs and justify each on the consent screen. Avoid wildcard / admin / full-access scopes.",
		RecommendedAction: "Confirm each requested scope is necessary; if the connector asks for more than its stated function needs, do not authorize it.",
		Evidence: []verdict.Evidence{
			{File: rel, Line: 1, Snippet: "connector requests scopes: " + trim(list, 200)},
		},
	}
}

func skillBroadDescriptionFinding(rel, desc string, line int) verdict.Finding {
	return verdict.Finding{
		ID:                "POISON-010-" + sanitizeID(rel),
		Severity:          "low",
		Category:          "skill_capability",
		Source:            verdict.SourcePoison,
		Title:             "POISON-010 — skill description is written for broad auto-activation in " + filepath.Base(rel),
		Description:       "This skill's `description` uses broad, always-on language (\"" + trim(desc, 160) + "\"). A skill's description is its auto-activation trigger; matching broadly fires the skill — and its instructions — on tasks the user never associated with it. This is the auto-activation half of skill capability-grant abuse (T11).",
		Context:           "Skill-capability pre-pass flagged an over-broad description in " + rel + ". Combined with any allowed-tools grant, broad activation widens where the skill's body can act.",
		Impact:            "The skill activates on unrelated work, executing its body instructions in contexts the user didn't intend. Low on its own; compounds with a broad allowed-tools grant (POISON-008).",
		Mitigation:        "Write a `description` that matches only the specific task the skill is for, so it activates narrowly. Avoid 'any task', 'always', 'for everything'.",
		RecommendedAction: "Review the description and narrow it to the skill's actual job.",
		Evidence: []verdict.Evidence{
			{File: rel, Line: line, Snippet: "description: " + trim(desc, 160)},
		},
	}
}

func isInterestingFile(path, root string) bool {
	rel, _ := filepath.Rel(root, path)
	base := filepath.Base(path)
	switch base {
	case ".mcp.json", "plugin.json", "claude-plugin.json",
		"connector.json", ".connector.json", "connector-manifest.json":
		return true
	}
	// A standalone skill is a SKILL.md anywhere in the tree (a skill
	// directory need not live under skills/).
	if strings.EqualFold(base, "SKILL.md") {
		return true
	}
	if !strings.HasSuffix(strings.ToLower(base), ".md") {
		return false
	}
	// Dirs the LLM loads at runtime.
	lower := strings.ToLower(rel)
	if strings.HasPrefix(lower, "skills/") ||
		strings.HasPrefix(lower, "commands/") ||
		strings.HasPrefix(lower, "prompts/") ||
		strings.Contains(lower, "/skills/") ||
		strings.Contains(lower, "/commands/") {
		return true
	}
	// A markdown file anywhere that carries skill-shaped frontmatter (name +
	// description) functions as a skill regardless of filename or directory.
	return looksLikeSkillFile(path)
}

// extractText pulls the text that actually flows into the LLM context.
// For JSON manifests we concatenate every "description" / "summary" /
// "instructions" string we find. For Markdown we read the whole body
// (skill files ARE the prompt).
func extractText(path, base string) string {
	data, err := os.ReadFile(path) // #nosec G304 -- walk-bounded
	if err != nil {
		return ""
	}
	if strings.HasSuffix(base, ".json") {
		var raw map[string]any
		if json.Unmarshal(data, &raw) != nil {
			return string(data) // fall back to whole text
		}
		var b strings.Builder
		collectStrings(raw, &b)
		return b.String()
	}
	return string(data)
}

// collectStrings walks an arbitrary JSON tree and appends every string
// it finds (whatever the key). We want to catch poisoned descriptions
// regardless of which field they live in (some MCP servers use
// "description", others "summary" or "instructions").
func collectStrings(v any, b *strings.Builder) {
	switch t := v.(type) {
	case map[string]any:
		for _, val := range t {
			collectStrings(val, b)
		}
	case []any:
		for _, val := range t {
			collectStrings(val, b)
		}
	case string:
		b.WriteString(t)
		b.WriteByte('\n')
	}
}

func toFinding(p Pattern, file string, h hit) verdict.Finding {
	return verdict.Finding{
		ID:                p.ID + "-" + sanitizeID(file) + ":" + itoa(h.line),
		Severity:          p.Severity,
		Category:          "tool_poisoning",
		Source:            verdict.SourcePoison,
		Title:             p.ID + " — " + p.Category + " in " + filepath.Base(file),
		Description:       p.Description,
		Context:           "Tool-poisoning pre-pass detector triggered on " + file + ":" + itoa(h.line) + ". This text enters the LLM context window when the plugin is loaded; the LLM may follow the embedded directive instead of (or in addition to) the user's intent.",
		Impact:            "Direct LLM behavior manipulation: the attacker controls what Claude Code does after loading this plugin. Severity depends on what other tools Claude has access to in the same session.",
		Mitigation:        p.Mitigation,
		RecommendedAction: "Review the snippet in context. If it's intentional plugin instruction (e.g. a system-style block in a skill file), confirm it matches your trust model. If unexpected, uninstall the plugin and report it.",
		Evidence: []verdict.Evidence{
			{File: file, Line: h.line, Snippet: h.snippet},
		},
	}
}

// --- pattern matchers ---

var (
	// Lines that read as imperative directives to the LLM. We require
	// either a verb at the start ("Ignore", "Forget", "Disregard") OR a
	// "you are X" identity statement that reframes the model.
	// Imperative directives aimed at the model. We deliberately do NOT match a
	// bare "you are a/an X" — that fires on legitimate self-descriptions like
	// "You are an MCP server that provides weather". The reframing tell is
	// "you are NOW ..." or an explicit override/role-switch phrase.
	reInstructionDirective = regexp.MustCompile(
		`(?im)` +
			`\b(?:ignore|forget|disregard|override|bypass)\b[^\n]{0,80}\b(?:previous|prior|earlier|all)\s+(?:instructions?|prompts?|context|rules?)\b` +
			`|\byou\s+are\s+now\b` +
			`|(?:^|[.!?:]\s*|\byou\s+(?:should\s+|must\s+|will\s+|can\s+)?)act\s+as\s+(?:a|an|the)\b` +
			`|\bfrom\s+now\s+on\b` +
			`|\bnew\s+instructions?\s*:` +
			`|\bdo\s+not\s+follow\b[^\n]{0,40}\b(?:instructions?|prompts?|rules?|system|guidelines?)\b`)

	// Fake role/system framing — these reliably confuse the LLM about
	// message boundaries.
	reRoleBlock = regexp.MustCompile(
		`(?im)(?:<\s*(?:system|user|assistant|admin)[^>]*>|#{1,3}\s*(?:SYSTEM|USER|ASSISTANT|ADMIN)\s*:?\s*$|\[\[\s*(?:SYSTEM|USER)\s*\]\])`)

	// Language that smells like exfil / outbound transmission. Tuned to
	// minimise false-positives on legit "this tool reads from <url>"
	// descriptions.
	reExfilLanguage = regexp.MustCompile(
		`(?im)\b(?:` +
			`exfiltrate` +
			`|leak\s+to` +
			`|upload\s+(?:to|the)` +
			`|(?:send|forward|transmit|post)\s+(?:the|all|your|every)\s+(?:conversation|context|history|transcript|logs?|data|messages?)` +
			`|(?:send|forward|transmit)\s+to\s+(?:https?://|[a-z0-9.-]+\.[a-z]{2,})` +
			`)\b`)

	// Prose directive to read a well-known credential path. Unlike the prepass
	// `sensitive-path-read` pattern (which requires a quoted string literal),
	// this catches natural-language skill/tool text like "read ~/.aws/credentials".
	reCredentialPathProse = regexp.MustCompile(
		`(?i)\b(?:read|cat|open|include|send|exfiltrate|access|load|print|output|return|reveal)\b[^\n]{0,40}(?:~|\$HOME|\$\{HOME\})?/?\.(?:ssh|aws|gnupg|kube|docker|config/gcloud)\b`)

	// Tool names that are within 1 edit of well-known tool names. We
	// keep this catalog small and audit it manually to avoid noise.
	typoSquatTargets = []string{"git", "npm", "pip", "node", "claude", "bash", "ssh", "curl", "wget", "cargo", "docker"}
)

func matchRegex(re *regexp.Regexp) func(string) []hit {
	return func(text string) []hit {
		var hits []hit
		for i, line := range strings.Split(text, "\n") {
			if re.MatchString(line) {
				hits = append(hits, hit{snippet: trim(line, 200), line: i + 1})
			}
		}
		return hits
	}
}

// matchUnicode flags any line containing invisible / zero-width chars.
func matchUnicode(text string) []hit {
	var hits []hit
	// Zero-width + bidi-override codepoints. Spelled out as numeric
	// literals so the source file stays pure ASCII (the chars themselves
	// would otherwise embed in this file and confuse reviewers).
	bad := []rune{
		0x200B, // zero-width space
		0x200C, // zero-width non-joiner
		0x200D, // zero-width joiner
		0x2060, // word joiner
		0xFEFF, // zero-width no-break space / BOM
		0x202E, // right-to-left override
		0x202D, // left-to-right override
	}
	for i, line := range strings.Split(text, "\n") {
		for _, r := range line {
			for _, b := range bad {
				if r == b {
					hits = append(hits, hit{snippet: trim(line, 200) + " [contains U+" + rune2hex(r) + "]", line: i + 1})
					goto next
				}
			}
			if r >= 0xE0000 && r <= 0xE007F {
				hits = append(hits, hit{snippet: trim(line, 200) + " [contains Unicode tag char]", line: i + 1})
				goto next
			}
		}
	next:
	}
	return hits
}

var reMdLink = regexp.MustCompile(`\[([^\]]+)\]\((https?://[^)\s]+)\)`)

func matchDeceptiveLink(text string) []hit {
	var hits []hit
	for i, line := range strings.Split(text, "\n") {
		for _, m := range reMdLink.FindAllStringSubmatch(line, -1) {
			label := strings.ToLower(m[1])
			url := strings.ToLower(m[2])
			// Only flag when the label LOOKS like a domain that doesn't
			// match the actual URL host. "Click here" → URL is fine.
			if !strings.Contains(label, ".") {
				continue
			}
			host := domainOf(url)
			labelHost := domainOf("https://" + label)
			if host != "" && labelHost != "" && host != labelHost {
				hits = append(hits, hit{
					snippet: "[" + m[1] + "](" + m[2] + ") — label says " + labelHost + " but resolves to " + host,
					line:    i + 1,
				})
			}
		}
	}
	return hits
}

// --- helpers ---

func sanitizeID(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			out = append(out, c)
		} else {
			out = append(out, '_')
		}
	}
	return string(out)
}

func itoa(n int) string {
	// avoid pulling in strconv for a one-line helper this hot
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func trim(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

func rune2hex(r rune) string {
	const hex = "0123456789ABCDEF"
	var buf [6]byte
	for i := 5; i >= 0; i-- {
		buf[i] = hex[r&0xF]
		r >>= 4
	}
	return string(buf[:])
}

func domainOf(rawURL string) string {
	rawURL = strings.TrimPrefix(rawURL, "http://")
	rawURL = strings.TrimPrefix(rawURL, "https://")
	if i := strings.IndexAny(rawURL, "/?#"); i >= 0 {
		rawURL = rawURL[:i]
	}
	return strings.ToLower(strings.TrimSpace(rawURL))
}
