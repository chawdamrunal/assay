package verdict

import (
	"strings"
	"testing"
	"time"
)

func sampleUnsafe() Verdict {
	return Verdict{
		SchemaVersion:   SchemaVersion,
		ScanID:          "s1",
		Target:          Target{Kind: "claude-code-plugin", Name: "my-slack-plugin"},
		ScannedAt:       time.Unix(0, 0).UTC(),
		Scanner:         Scanner{Name: ScannerName, Version: "0.1.0", PromptVersion: "mcp-v2"},
		Verdict:         "unsafe",
		Summary:         "Reads ~/.aws/credentials and POSTs to an undeclared host.",
		ClaimsVsReality: "declares read-only Slack -> actually reads AWS creds + egresses.\n(more detail)",
		Findings: []Finding{
			// Deliberately first and unknown-severity: pre-fix, a missing
			// cardSeverityRank entry evaluates to 0 (Go zero value), which
			// ties "critical"'s rank — combined with sort.SliceStable and its
			// position here (before the real critical finding), the bug
			// would leave it sorted at the top instead of the bottom.
			{Severity: "", Title: "Unknown severity should sort to bottom, not top", Evidence: []Evidence{{File: "src/unknown.ts", Line: 7}}},
			{Severity: "high", Title: "Undeclared network egress", Evidence: []Evidence{{File: "src/index.ts", Line: 88}}},
			{Severity: "critical", Title: "Credential exfil", Evidence: []Evidence{{File: "hooks/session.mjs", Line: 42}}},
			{Severity: "medium", Title: "axios@0.21.1 CVE-2021-3749", Source: SourceSCA},
		},
	}
}

func TestRenderCard_UnsafeSortsAndTagsFloor(t *testing.T) {
	got := RenderCard(sampleUnsafe(), CardOptions{DroppedCount: 1, ReportURL: "https://x/report"})
	// Header shows badge + verdict + name.
	if !strings.HasPrefix(got, "### ⛔ Assay: unsafe — my-slack-plugin") {
		t.Fatalf("bad header:\n%s", got)
	}
	// Critical is sorted above high.
	ci, hi := strings.Index(got, "CRITICAL"), strings.Index(got, "HIGH")
	if ci < 0 || hi < 0 || ci > hi {
		t.Fatalf("severity order wrong (crit=%d high=%d):\n%s", ci, hi, got)
	}
	// Unknown/empty severity must not out-rank a real critical finding: it
	// ties critical's zero-value rank pre-fix but must sort last post-fix.
	if ui := strings.Index(got, "Unknown severity should sort to bottom"); ui < 0 || ui < ci {
		t.Fatalf("unknown-severity finding missing or sorted above critical (unknown=%d crit=%d):\n%s", ui, ci, got)
	}
	// First-evidence location is rendered.
	if !strings.Contains(got, "hooks/session.mjs:42") {
		t.Fatalf("missing evidence location:\n%s", got)
	}
	// Floor finding is tagged.
	if !strings.Contains(got, "[floor]") {
		t.Fatalf("missing [floor] tag:\n%s", got)
	}
	// Claims-vs-reality is present and truncated to its first line.
	if !strings.Contains(got, "Claims vs reality:") {
		t.Fatalf("missing claims vs reality line:\n%s", got)
	}
	if strings.Contains(got, "(more detail)") {
		t.Fatalf("claims vs reality not truncated to first line:\n%s", got)
	}
	// LLM-tier footer shows dropped count, prompt version, report link.
	if !strings.Contains(got, "1 dropped as unverified") ||
		!strings.Contains(got, "prompt mcp-v2") ||
		!strings.Contains(got, "https://x/report") {
		t.Fatalf("bad footer:\n%s", got)
	}
}

func TestRenderCard_NoLLMOmitsHallucinationFooter(t *testing.T) {
	v := sampleUnsafe()
	v.Scanner.PromptVersion = "no-llm"
	// DroppedCount > 0 on purpose: a no-llm run has no citation validator, so
	// nothing should ever be attributed to it — this forces the pre-fix
	// footer code path so the assertion actually proves the llmTier gate.
	got := RenderCard(v, CardOptions{DroppedCount: 2})
	if strings.Contains(got, "dropped as unverified") || strings.Contains(got, "prompt ") {
		t.Fatalf("no-llm card must omit hallucination-guard footer:\n%s", got)
	}
	if !strings.Contains(got, "### ⛔ Assay: unsafe") {
		t.Fatalf("no-llm card still needs a header:\n%s", got)
	}
}
