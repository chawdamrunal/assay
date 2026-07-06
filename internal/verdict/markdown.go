package verdict

import (
	"fmt"
	"sort"
	"strings"
)

// severityRank orders findings highest severity first.
var severityRank = map[string]int{
	"critical": 4,
	"high":     3,
	"medium":   2,
	"low":      1,
	"info":     0,
}

// RenderMarkdown produces an audit.md from a Verdict. This is the deterministic
// fallback used when the synthesis stage couldn't produce one (e.g., budget
// exceeded). The LLM-produced audit.md is preferred when available — it carries
// the model's narrative; this renderer carries only the structured data.
func RenderMarkdown(v Verdict) string {
	var b strings.Builder

	// Header. Artifact-controlled fields (target name/version, and every finding
	// value/evidence field below) are escaped with mdInline — collapse newlines
	// and control chars, neutralize backticks — so a crafted plugin name, finding
	// title, or evidence snippet cannot inject block-level markdown (e.g. a spoofed
	// "## ✅ VERDICT: SAFE" header) into this report. The narrative sections
	// (Summary, DataFlowDiagram, ThreatModel, ClaimsVsReality) are intentionally
	// model-produced markdown and are deliberately rendered as-is.
	fmt.Fprintf(&b, "# Assay Security Audit — %s", mdInline(v.Target.Name))
	if v.Target.Version != "" {
		fmt.Fprintf(&b, " v%s", mdInline(v.Target.Version))
	}
	b.WriteString("\n\n")

	verdictUpper := strings.ToUpper(v.Verdict)
	fmt.Fprintf(&b, "**Verdict:** %s\n", verdictUpper)
	if !v.ScannedAt.IsZero() {
		fmt.Fprintf(&b, "**Scanned:** %s\n", v.ScannedAt.Format("2006-01-02 15:04:05 UTC"))
	}
	fmt.Fprintf(&b, "**Scanner:** %s v%s using %s with prompts %s\n\n",
		v.Scanner.Name, v.Scanner.Version, v.Scanner.Model, v.Scanner.PromptVersion)

	// Executive summary
	if v.Summary != "" {
		b.WriteString("## Executive Summary\n\n")
		b.WriteString(v.Summary)
		b.WriteString("\n\n")
	}

	// Data-flow diagram (Mermaid). Comes BEFORE the threat model because the
	// threat model references its nodes.
	if strings.TrimSpace(v.DataFlowDiagram) != "" {
		b.WriteString("## Data Flow\n\n")
		b.WriteString(v.DataFlowDiagram)
		if !strings.HasSuffix(v.DataFlowDiagram, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	// Threat model
	if strings.TrimSpace(v.ThreatModel) != "" {
		b.WriteString("## Threat Model\n\n")
		b.WriteString(v.ThreatModel)
		if !strings.HasSuffix(v.ThreatModel, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	// Claims vs reality
	if strings.TrimSpace(v.ClaimsVsReality) != "" {
		b.WriteString("## Claims vs. Reality\n\n")
		b.WriteString(v.ClaimsVsReality)
		b.WriteString("\n\n")
	}

	// Findings
	b.WriteString("## Findings\n\n")
	if len(v.Findings) == 0 {
		b.WriteString("No findings. This artifact passed all investigations.\n\n")
	} else {
		findings := make([]Finding, len(v.Findings))
		copy(findings, v.Findings)
		sort.SliceStable(findings, func(i, j int) bool {
			return severityRank[findings[i].Severity] > severityRank[findings[j].Severity]
		})
		for _, f := range findings {
			renderFinding(&b, f)
		}
	}

	// Open questions
	if len(v.OpenQuestions) > 0 {
		b.WriteString("## Open Questions\n\n")
		for _, q := range v.OpenQuestions {
			fmt.Fprintf(&b, "- %s\n", mdInline(q))
		}
		b.WriteString("\n")
	}

	// Audit metadata
	b.WriteString("## Audit Metadata\n\n")
	fmt.Fprintf(&b, "- Scan ID: `%s`\n", v.ScanID)
	if v.Target.Hash != "" {
		fmt.Fprintf(&b, "- Target hash: `%s`\n", v.Target.Hash)
	}
	if v.Target.Source != "" {
		fmt.Fprintf(&b, "- Source: %s\n", v.Target.Source)
	}
	fmt.Fprintf(&b, "- Model: %s\n", v.Scanner.Model)
	fmt.Fprintf(&b, "- Prompt version: %s\n", v.Scanner.PromptVersion)
	fmt.Fprintf(&b, "- Schema version: %s\n", v.SchemaVersion)

	return b.String()
}

func renderFinding(b *strings.Builder, f Finding) {
	severityUpper := strings.ToUpper(f.Severity)
	fmt.Fprintf(b, "### %s: %s [%s]\n\n", mdInline(f.ID), mdInline(f.Title), severityUpper)
	if f.Category != "" {
		fmt.Fprintf(b, "**Category:** %s\n\n", mdInline(f.Category))
	}
	if f.Description != "" {
		fmt.Fprintf(b, "**Description:** %s\n\n", mdInline(f.Description))
	}
	if f.Context != "" {
		fmt.Fprintf(b, "**Context:** %s\n\n", mdInline(f.Context))
	}
	if len(f.Evidence) > 0 {
		b.WriteString("**Evidence:**\n\n")
		for _, e := range f.Evidence {
			fmt.Fprintf(b, "- `%s:%d` — `%s`\n", mdInline(e.File), e.Line, mdInline(e.Snippet))
		}
		b.WriteString("\n")
	}
	if f.Impact != "" {
		fmt.Fprintf(b, "**Impact:** %s\n\n", mdInline(f.Impact))
	}
	if f.Mitigation != "" {
		fmt.Fprintf(b, "**Mitigation:** %s\n\n", mdInline(f.Mitigation))
	}
	if f.ExploitScenario != "" {
		fmt.Fprintf(b, "**Exploit scenario:** %s\n\n", mdInline(f.ExploitScenario))
	}
	if f.RecommendedAction != "" {
		fmt.Fprintf(b, "**Recommended action:** %s\n\n", mdInline(f.RecommendedAction))
	}
}
