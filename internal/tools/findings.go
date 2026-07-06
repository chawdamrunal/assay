package tools

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
)

// Severity values accepted by record_finding.
var validSeverities = map[string]int{
	"critical": 4,
	"high":     3,
	"medium":   2,
	"low":      1,
	"info":     0,
}

// Categories the agent may use. "other" is the catch-all.
var validCategories = map[string]bool{
	"exfiltration":     true,
	"injection":        true,
	"overscope":        true,
	"supply-chain":     true,
	"secret":           true,
	"prompt-injection": true,
	"hook-abuse":       true,
	"settings-drift":   true,
	"other":            true,
}

// EvidenceEntry is one (file, line, snippet) citation supporting a finding.
type EvidenceEntry struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Snippet string `json:"snippet"`
}

// Finding is the structured output from record_finding before post-validation.
type Finding struct {
	Severity        string          `json:"severity"`
	Category        string          `json:"category"`
	Title           string          `json:"title"`
	Description     string          `json:"description,omitempty"`
	Evidence        []EvidenceEntry `json:"evidence,omitempty"`
	ExploitScenario string          `json:"exploit_scenario,omitempty"`
	ThreatID        string          `json:"threat_id,omitempty"` // set by dispatch_subagent on behalf of investigator
}

// Findings is a thread-safe collector for record_finding tool invocations.
type Findings struct {
	mu       sync.Mutex
	findings []Finding
	threatID string // optional context: when set, every Record stamps this threat_id
}

// NewFindings returns an empty collector.
func NewFindings() *Findings { return &Findings{} }

// WithThreatID returns a child collector that stamps threatID onto every
// recorded finding. Used by dispatch_subagent to attribute findings to a threat.
func (f *Findings) WithThreatID(threatID string) *Findings {
	return &Findings{threatID: threatID, findings: nil}
}

// Def returns the agent-facing tool definition.
func (f *Findings) Def() Tool {
	return Tool{
		Name:        "record_finding",
		Description: "Record a security finding with severity, category, title, description, exploit scenario, and evidence (file/line/verbatim snippet). Non-info severities require at least one evidence entry. To report 'no issues found' for a threat, use severity=info with empty evidence.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"severity": map[string]any{
					"type": "string",
					"enum": []string{"critical", "high", "medium", "low", "info"},
				},
				"category": map[string]any{
					"type": "string",
					"enum": []string{
						"exfiltration", "injection", "overscope", "supply-chain",
						"secret", "prompt-injection", "hook-abuse", "settings-drift", "other",
					},
				},
				"title":            map[string]any{"type": "string"},
				"description":      map[string]any{"type": "string"},
				"exploit_scenario": map[string]any{"type": "string"},
				"evidence": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"file":    map[string]any{"type": "string"},
							"line":    map[string]any{"type": "integer", "minimum": 1},
							"snippet": map[string]any{"type": "string", "description": "Verbatim quote from the file at the cited line. Will be re-validated against the file content."},
						},
						"required": []string{"file", "line", "snippet"},
					},
				},
			},
			"required": []string{"severity", "category", "title"},
		},
	}
}

// Record validates and appends one finding.
func (f *Findings) Record(_ context.Context, in Invocation) (Result, error) {
	severity, _ := in.Input["severity"].(string)
	if _, ok := validSeverities[severity]; !ok {
		return Result{}, fmt.Errorf("record_finding: invalid severity %q (must be one of critical|high|medium|low|info)", severity)
	}
	category, _ := in.Input["category"].(string)
	if !validCategories[category] {
		return Result{}, fmt.Errorf("record_finding: invalid category %q", category)
	}
	title, _ := in.Input["title"].(string)
	if title == "" {
		return Result{}, errors.New("record_finding: title is required")
	}

	finding := Finding{
		Severity:        severity,
		Category:        category,
		Title:           title,
		Description:     stringArg(in.Input, "description"),
		ExploitScenario: stringArg(in.Input, "exploit_scenario"),
		ThreatID:        f.threatID,
	}

	rawEvidence, _ := in.Input["evidence"].([]any)
	for _, e := range rawEvidence {
		obj, ok := e.(map[string]any)
		if !ok {
			continue
		}
		file, _ := obj["file"].(string)
		line := intArg(obj, "line")
		snippet, _ := obj["snippet"].(string)
		if file == "" || line == 0 || snippet == "" {
			return Result{}, fmt.Errorf("record_finding: evidence entry must have file, line, and snippet")
		}
		finding.Evidence = append(finding.Evidence, EvidenceEntry{
			File:    file,
			Line:    line,
			Snippet: snippet,
		})
	}

	if severity != "info" && len(finding.Evidence) == 0 {
		return Result{}, fmt.Errorf("record_finding: severity=%q requires at least one evidence entry (file/line/verbatim snippet)", severity)
	}

	f.mu.Lock()
	f.findings = append(f.findings, finding)
	f.mu.Unlock()

	return Result{Text: fmt.Sprintf("recorded finding: [%s] %s", severity, title)}, nil
}

// All returns a copy of all recorded findings.
func (f *Findings) All() []Finding {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Finding, len(f.findings))
	copy(out, f.findings)
	return out
}

// Merge appends other's findings into f. Used by dispatch_subagent to roll up sub-agent results.
func (f *Findings) Merge(other *Finding) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.findings = append(f.findings, *other)
}

// MergeAll appends every finding from other.
func (f *Findings) MergeAll(other *Findings) {
	all := other.All()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.findings = append(f.findings, all...)
}

// SortBySeverity sorts findings in place, highest severity first.
func (f *Findings) SortBySeverity() {
	f.mu.Lock()
	defer f.mu.Unlock()
	sort.SliceStable(f.findings, func(i, j int) bool {
		return validSeverities[f.findings[i].Severity] > validSeverities[f.findings[j].Severity]
	})
}

func stringArg(in map[string]any, key string) string {
	v, _ := in[key].(string)
	return v
}
