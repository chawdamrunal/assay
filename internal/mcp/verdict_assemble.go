package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chawdamrunal/assay/internal/floor"
	"github.com/chawdamrunal/assay/internal/inventory"
	"github.com/chawdamrunal/assay/internal/verdict"
)

// assembleVerdict turns the raw inputs Claude provides via assay_finalize_scan
// into the public verdict.Verdict shape, runs the citation post-validator
// (re-reading each cited file:line to confirm the verbatim snippet is real),
// renders audit.md, and returns both as bytes ready to write.
//
// This is what enforces the "no quote, no finding" rule end-to-end: any
// finding whose evidence cannot be re-located on disk is silently dropped
// from the final audit, and the over-all verdict is recomputed from what
// survives validation.
func assembleVerdict(
	ctx context.Context,
	scanID, target, verdictLabel, summary, dataFlowDiagram, threatModel, claimsVsReality, model string,
	offline bool,
	rawFindings []map[string]any,
) (auditJSON []byte, auditMarkdown string, err error) {
	findings := make([]verdict.Finding, 0, len(rawFindings))
	for _, f := range rawFindings {
		findings = append(findings, rawToFinding(f))
	}
	validated, _ := verdict.Validate(target, findings)
	if validated == nil {
		validated = []verdict.Finding{}
	}
	// Verdict the LLM's own (validated) findings justify, BEFORE the
	// deterministic floor runs. Used below to keep the executive summary
	// honest when the floor raises the verdict.
	llmVerdict := recomputeVerdict(validated, verdictLabel)

	// Deterministic floor: SCA (transitive CVE) + poison (tool-poisoning)
	// findings, appended AFTER the LLM stages through the same schema. Shared
	// with the legacy/CLI path via internal/floor so every scan mode produces
	// the same floor. The SCA timeout derives from the caller's ctx so a
	// cancelled scan (user deleted it, server shutting down) stops the OSV HTTP
	// work. offline is threaded from the MCP server's --offline flag so an
	// air-gapped scan skips the OSV.dev lookup here too (not just in the prompt).
	validated = floor.Apply(ctx, target, offline, validated)

	// Populate target.Hash from the actual scanned tree. This makes diff-mode
	// matching content-aware: two installs of the same plugin version at
	// different paths share a hash, so diff finds the right baseline. Best-
	// effort — failures are non-fatal (e.g. permission denied on a sub-dir).
	targetHash := ""
	if h, err := inventory.HashDir(target); err == nil {
		targetHash = h
	}

	// Recompute the verdict over the floor-augmented set. If the deterministic
	// floor (SCA/poison) raised it above what the LLM concluded, prepend a
	// note so the executive summary can't read "safe" while CVEs sit below.
	finalVerdict := recomputeVerdict(validated, verdictLabel)
	summaryOut := summary
	if verdictRank(finalVerdict) > verdictRank(llmVerdict) {
		summaryOut = "Note: Assay raised this verdict to " + finalVerdict +
			" via its deterministic SCA/poison floor after the LLM review (the model assessed the code as " + llmVerdict +
			"). See the findings below.\n\n" + summary
	}

	v := verdict.Verdict{
		SchemaVersion: verdict.SchemaVersion,
		ScanID:        scanID,
		Target: verdict.Target{
			Kind:   deriveTargetKind(target),
			Name:   filepath.Base(target),
			Source: "local://" + target,
			Hash:   targetHash,
		},
		ScannedAt: time.Now().UTC(),
		Scanner: verdict.Scanner{
			Name:          verdict.ScannerName,
			Version:       Version,
			Model:         model,
			PromptVersion: "mcp-v2",
		},
		Verdict:         finalVerdict,
		Summary:         summaryOut,
		DataFlowDiagram: dataFlowDiagram,
		ThreatModel:     threatModel,
		ClaimsVsReality: claimsVsReality,
		Findings:        validated,
	}

	jsonBytes, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, "", err
	}
	md := verdict.RenderMarkdown(v)
	return jsonBytes, md, nil
}

// deriveTargetKind classifies the scanned artifact for the verdict's
// target.kind deterministically from what is on disk — not from the LLM, which
// can be wrong or omit it. Precedence: a plugin manifest wins (a plugin bundle
// can itself contain skills and an .mcp.json), then a standalone skill, then an
// MCP server, then a connector manifest; otherwise "other". The returned values
// mirror the kind enum in schemas/verdict-v0.1.json.
func deriveTargetKind(target string) string {
	info, err := os.Stat(target)
	if err != nil {
		return "other"
	}
	dir := target
	if !info.IsDir() {
		base := strings.ToLower(filepath.Base(target))
		switch {
		case base == "skill.md":
			return "skill"
		case strings.HasSuffix(base, ".mcp.json"):
			return "mcp-server"
		}
		dir = filepath.Dir(target)
	}
	has := func(name string) bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	}
	switch {
	case has("plugin.json") || has("claude-plugin.json"):
		return "claude-code-plugin"
	case has("SKILL.md"):
		return "skill"
	case has(".mcp.json"):
		return "mcp-server"
	case isConnectorManifestDir(dir):
		return "connector"
	default:
		return "other"
	}
}

// isConnectorManifestDir reports whether dir holds a connector manifest — a
// JSON file declaring OAuth scopes / an auth or base URL. Connectors are
// hosted/closed-source, so there is no canonical local layout; we recognize the
// conventional manifest names and require a scope/oauth/url signal to avoid
// misclassifying an unrelated JSON file.
func isConnectorManifestDir(dir string) bool {
	for _, name := range []string{"connector.json", ".connector.json", "connector-manifest.json"} {
		data, err := os.ReadFile(filepath.Join(dir, name)) // #nosec G304 -- fixed names under the scan dir
		if err != nil {
			continue
		}
		var m map[string]any
		if json.Unmarshal(data, &m) != nil {
			return true // a connector.json that is present but unparseable is still a connector signal
		}
		for _, k := range []string{"scopes", "oauth", "auth_url", "authorization_url", "base_url", "baseUrl"} {
			if _, ok := m[k]; ok {
				return true
			}
		}
	}
	return false
}

// rawToFinding maps the agent-supplied map[string]any into a typed Finding.
// Unknown fields are dropped; missing optionals are left zero.
func rawToFinding(m map[string]any) verdict.Finding {
	f := verdict.Finding{
		ID:                stringOf(m["id"]),
		Severity:          stringOf(m["severity"]),
		Category:          stringOf(m["category"]),
		Title:             stringOf(m["title"]),
		Description:       stringOf(m["description"]),
		Context:           stringOf(m["context"]),
		Impact:            stringOf(m["impact"]),
		Mitigation:        stringOf(m["mitigation"]),
		ExploitScenario:   stringOf(m["exploit_scenario"]),
		RecommendedAction: stringOf(m["recommended_action"]),
		ThreatID:          stringOf(m["threat_id"]),
	}
	if ev, ok := m["evidence"].([]any); ok {
		for _, e := range ev {
			em, ok := e.(map[string]any)
			if !ok {
				continue
			}
			f.Evidence = append(f.Evidence, verdict.Evidence{
				File:    stringOf(em["file"]),
				Line:    intOf(em["line"]),
				Snippet: stringOf(em["snippet"]),
			})
		}
	}
	return f
}

// recomputeVerdict downgrades the agent-claimed verdict if validation
// removed evidence that would justify it. Conservative: never upgrades.
func recomputeVerdict(findings []verdict.Finding, claimed string) string {
	hasUnsafe := false
	hasCaution := false
	for _, f := range findings {
		sev := strings.ToLower(f.Severity)
		switch sev {
		case "critical", "high":
			hasUnsafe = true
		case "medium":
			hasCaution = true
		}
	}
	switch {
	case hasUnsafe:
		return "unsafe"
	case hasCaution:
		return "caution"
	case claimed == "unsafe" || claimed == "caution":
		// Agent claimed harm but nothing survived validation — downgrade.
		return "safe"
	default:
		return "safe"
	}
}

// verdictRank orders verdict labels so we can tell when the floor raised one
// (safe < caution < unsafe).
func verdictRank(v string) int {
	switch v {
	case "unsafe":
		return 2
	case "caution":
		return 1
	default:
		return 0
	}
}

func stringOf(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func intOf(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}
	return 0
}
