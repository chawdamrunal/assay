// Package scanner orchestrates the 5-stage Sonnet-driven security review.
package scanner

// TriageMap is the output of Stage 0.
type TriageMap struct {
	DeclaredKind    string   `json:"declared_kind"`
	DeclaredPurpose string   `json:"declared_purpose"`
	EntryPoints     []string `json:"entry_points"`
	Permissions     []string `json:"permissions"`
	FilesToInspect  []string `json:"files_to_inspect"`
	Boilerplate     []string `json:"boilerplate"`
	Notes           string   `json:"notes"`
}

// Claims is the output of Stage 1.
type Claims struct {
	ClaimsParagraph      string   `json:"claims_paragraph"`
	DeclaredCapabilities []string `json:"declared_capabilities"`
	DeclaredPermissions  []string `json:"declared_permissions"`
	DeclaredNetwork      []string `json:"declared_network"`
	DeclaredDependencies []string `json:"declared_dependencies"`
	TrustSignals         []string `json:"trust_signals"`
}

// Threat is a single threat in the model.
type Threat struct {
	ID                string   `json:"id"`
	Title             string   `json:"title"`
	Class             string   `json:"class"`
	Severity          string   `json:"severity"`
	Description       string   `json:"description"`
	ReviewerQuestions []string `json:"reviewer_questions"`
}

// ThreatModel is the output of Stage 2.
type ThreatModel struct {
	RawMarkdown string   `json:"raw_markdown"`
	Threats     []Threat `json:"threats"`
}

// Verdict is the final output of a scan. The verdict package (P2-T18) defines
// the full public schema; this is the minimal in-package representation the
// orchestrator returns.
type Verdict struct {
	ScanID        string       `json:"scan_id"`
	Target        string       `json:"target"`
	Verdict       string       `json:"verdict"` // safe | caution | unsafe
	Summary       string       `json:"summary"`
	ThreatModel   ThreatModel  `json:"threat_model"`
	Claims        Claims       `json:"claims"`
	Findings      []FindingOut `json:"findings"`
	OpenQuestions []string     `json:"open_questions"`
	AuditMarkdown string       `json:"audit_markdown"`
	Model         string       `json:"model"`
	PromptVersion string       `json:"prompt_version"`
}

// FindingOut is a finding ready for serialization (re-exported tools.Finding shape).
type FindingOut struct {
	ID          string        `json:"id"`
	Severity    string        `json:"severity"`
	Category    string        `json:"category"`
	Title       string        `json:"title"`
	Description string        `json:"description,omitempty"`
	Evidence    []EvidenceOut `json:"evidence,omitempty"`
	// Context / Impact / Mitigation / RecommendedAction mirror the public
	// verdict.Finding fields so the legacy (in-process) scanner can populate
	// the same rich panels the MCP path does — previously these were dropped
	// at the FromScanner boundary and the UI rendered blank sections.
	Context           string `json:"context,omitempty"`
	Impact            string `json:"impact,omitempty"`
	Mitigation        string `json:"mitigation,omitempty"`
	ExploitScenario   string `json:"exploit_scenario,omitempty"`
	RecommendedAction string `json:"recommended_action,omitempty"`
	ThreatID          string `json:"threat_id,omitempty"`
}

// EvidenceOut is one citation in a finding.
type EvidenceOut struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Snippet string `json:"snippet"`
}

// Event is a progress event emitted during a scan.
type Event struct {
	Stage   string `json:"stage"`  // "prepass" | "triage" | "claims" | "threat_model" | "investigation" | "exploitability" | "synthesis" | "done"
	Status  string `json:"status"` // "start" | "complete" | "error"
	Message string `json:"message,omitempty"`
	// At is the RFC3339Nano timestamp the event was emitted, carried through
	// from the MCP ProgressEvent so the live UI can show per-stage timing.
	// omitempty keeps legacy in-process events (which don't set it) unchanged.
	At string `json:"at,omitempty"`
}

// Options controls one scan.
type Options struct {
	Target              string
	ClaudeDir           string // for inventory resolution
	Model               string
	ModelInvestigation  string
	BudgetUSD           float64
	SubagentConcurrency int
	Offline             bool
}
