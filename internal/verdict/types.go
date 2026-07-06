// Package verdict defines the public, schema-validated representation of an
// Assay security audit verdict. The schema is at schemas/verdict-v0.1.json.
//
// The scanner package produces an internal Verdict; this package is the
// serialization seam: scanner.Verdict + scan metadata -> verdict.Verdict
// (matches the public schema) -> audit.json + audit.md on disk.
package verdict

import (
	"time"

	"github.com/chawdamrunal/assay/internal/scanner"
)

// SchemaVersion is the current verdict schema version. Pinned in JSON output.
const SchemaVersion = "0.1"

// ScannerName is the constant identifier emitted in scanner.name.
const ScannerName = "assay"

// Finding.Source values. Empty == SourceLLM for back-compat.
const (
	SourceLLM    = "llm"    // model investigation; evidence is citation-validated
	SourceSCA    = "sca"    // deterministic dependency-CVE floor (synthetic evidence)
	SourcePoison = "poison" // deterministic tool-poisoning floor (synthetic evidence)
	SourceSecret = "secret" // deterministic plaintext-secret hit (real file:line evidence)
)

// PromptVersionNoLLM marks Scanner.PromptVersion for a deterministic-only
// scan (no LLM investigation stage ran, e.g. --no-llm). RenderCard gates the
// hallucination-guard footer segments (dropped-count and prompt-version) on
// PromptVersion being neither empty nor this value.
const PromptVersionNoLLM = "no-llm"

// Verdict is the schema-validated public verdict. Field tags match
// schemas/verdict-v0.1.json.
type Verdict struct {
	SchemaVersion   string    `json:"schema_version"`
	ScanID          string    `json:"scan_id"`
	Target          Target    `json:"target"`
	ScannedAt       time.Time `json:"scanned_at"`
	Scanner         Scanner   `json:"scanner"`
	Verdict         string    `json:"verdict"`
	Summary         string    `json:"summary,omitempty"`
	DataFlowDiagram string    `json:"data_flow_diagram,omitempty"` // Mermaid flowchart, produced before threat model
	ThreatModel     string    `json:"threat_model,omitempty"`
	ClaimsVsReality string    `json:"claims_vs_reality,omitempty"`
	Findings        []Finding `json:"findings"`
	OpenQuestions   []string  `json:"open_questions,omitempty"`
	Signatures      []string  `json:"signatures,omitempty"`
	// PriorScanID, when set, identifies the previous scan against which this
	// verdict was diffed. Per-finding Diff annotations are populated relative
	// to that scan. Empty when no comparison was requested or possible.
	PriorScanID string `json:"prior_scan_id,omitempty"`
}

// DiffAnnotation tags a finding with its status relative to a prior scan of
// the same target. Populated by verdict.Diff after the scan completes.
//
// Status values:
//   - "new"      — present in current, no match in prior
//   - "stable"   — present in both, no material change
//   - "changed"  — present in both, but description / severity / evidence drifted
//   - "resolved" — present in prior, absent in current (returned separately
//     in verdict.Diff's resolved slice, not attached to a
//     surviving Finding)
type DiffAnnotation struct {
	Status    string `json:"status"`
	SinceScan string `json:"since_scan,omitempty"`
	// PriorID, when set, points at the matching prior-scan finding's ID. Used
	// by the FE diff view to anchor side-by-side jumps.
	PriorID string `json:"prior_id,omitempty"`
}

// Target is what was scanned.
type Target struct {
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	Source  string `json:"source,omitempty"`
	Hash    string `json:"hash,omitempty"`
}

// Scanner identifies the Assay run that produced this verdict.
type Scanner struct {
	Name          string `json:"name"`
	Version       string `json:"version"`
	Model         string `json:"model"`
	PromptVersion string `json:"prompt_version"`
}

// Finding is one security finding in the verdict.
//
// The four "context-aware" fields (Context, Impact, Mitigation, in addition
// to ExploitScenario) are mandatory in MCP-mode output — the methodology
// prompt instructs Claude to fill all of them with target-specific text, not
// boilerplate. Older legacy-mode scans may omit them; the renderer copes.
type Finding struct {
	ID          string     `json:"id"`
	Severity    string     `json:"severity"`
	Category    string     `json:"category"`
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	Evidence    []Evidence `json:"evidence,omitempty"`
	// Context says WHERE in the artifact's data flow this finding lives
	// (which input source, which trust boundary, which sink). References the
	// data-flow diagram's node names when present.
	Context string `json:"context,omitempty"`
	// Impact is the concrete consequence for THIS target — names the
	// affected data, the affected users, and the business-/compliance-level
	// outcome. No boilerplate.
	Impact string `json:"impact,omitempty"`
	// Mitigation names the specific framework/library/API the target already
	// uses and the exact code change. Not generic "validate inputs."
	Mitigation      string `json:"mitigation,omitempty"`
	ExploitScenario string `json:"exploit_scenario,omitempty"`
	// RecommendedAction is the high-level "what to do now" the reader can
	// act on without reading code (e.g. uninstall, rotate keys, etc.).
	RecommendedAction string `json:"recommended_action,omitempty"`
	ThreatID          string `json:"threat_id,omitempty"`
	// Source records who produced this finding: SourceLLM (the model's
	// investigation, whose evidence IS citation-validated against the source
	// tree) or a deterministic floor — SourceSCA / SourcePoison, whose
	// evidence is a synthetic manifest reference, not a source-code claim —
	// or SourceSecret, whose evidence IS real verbatim file:line evidence
	// (unlike SCA/poison), just produced deterministically rather than by the
	// model. Empty is treated as SourceLLM for back-compat. The citation
	// validator exempts every non-SourceLLM finding from the file-content
	// re-read via a blanket Source != SourceLLM check, so the exemption
	// applies uniformly regardless of whether the evidence is synthetic or
	// real.
	Source string `json:"source,omitempty"`
	// Diff, when set, annotates this finding relative to a prior scan of the
	// same target. Populated by verdict.Diff; nil when the scan was not
	// compared (the common case until the user opts into diff mode).
	Diff *DiffAnnotation `json:"diff,omitempty"`
}

// Evidence is one (file, line, snippet) citation supporting a finding.
type Evidence struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Snippet string `json:"snippet"`
}

// FromScanner converts a scanner.Verdict into the public-schema Verdict shape.
// targetKind, targetName, targetHash come from the scanner driver (CLI/API)
// since scanner.Verdict carries only the file-system path.
func FromScanner(v *scanner.Verdict, target Target, scannerVersion string) Verdict {
	out := Verdict{
		SchemaVersion: SchemaVersion,
		ScanID:        v.ScanID,
		Target:        target,
		ScannedAt:     time.Now().UTC(),
		Scanner: Scanner{
			Name:          ScannerName,
			Version:       scannerVersion,
			Model:         v.Model,
			PromptVersion: v.PromptVersion,
		},
		Verdict:       v.Verdict,
		Summary:       v.Summary,
		ThreatModel:   v.ThreatModel.RawMarkdown,
		OpenQuestions: v.OpenQuestions,
	}
	for _, f := range v.Findings {
		fout := Finding{
			ID:                f.ID,
			Severity:          f.Severity,
			Category:          f.Category,
			Title:             f.Title,
			Description:       f.Description,
			Context:           f.Context,
			Impact:            f.Impact,
			Mitigation:        f.Mitigation,
			ExploitScenario:   f.ExploitScenario,
			RecommendedAction: f.RecommendedAction,
			ThreatID:          f.ThreatID,
		}
		for _, e := range f.Evidence {
			fout.Evidence = append(fout.Evidence, Evidence{File: e.File, Line: e.Line, Snippet: e.Snippet})
		}
		out.Findings = append(out.Findings, fout)
	}
	if out.Findings == nil {
		out.Findings = []Finding{}
	}
	return out
}
