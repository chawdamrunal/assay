package scanner

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/chawdamrunal/assay/internal/claude"
	"github.com/chawdamrunal/assay/internal/prompts"
	"github.com/chawdamrunal/assay/internal/tools"
)

// SynthesisInput holds dependencies for Stage 5.
type SynthesisInput struct {
	Client        claude.Client
	Model         string
	Target        string
	Claims        Claims
	ThreatModel   ThreatModel
	Findings      []tools.Finding
	OpenQuestions []string
}

// RunSynthesis executes Stage 5 — produce the audit markdown and final Verdict.
func RunSynthesis(ctx context.Context, in SynthesisInput) (Verdict, error) {
	system, err := prompts.Load(prompts.Version, "synthesis")
	if err != nil {
		return Verdict{}, fmt.Errorf("synthesis: load prompt: %w", err)
	}

	claimsJSON, _ := json.Marshal(in.Claims)
	tmJSON, _ := json.Marshal(in.ThreatModel)
	findingsJSON, _ := json.Marshal(in.Findings)
	openJSON, _ := json.Marshal(in.OpenQuestions)
	userMsg := fmt.Sprintf(`Target: %s

Claims:
%s

Threat model:
%s

Findings:
%s

Open questions:
%s

Produce the final audit markdown now.`, in.Target, string(claimsJSON), string(tmJSON), string(findingsJSON), string(openJSON))

	agent := &claude.Agent{
		Client:   in.Client,
		Model:    in.Model,
		System:   system,
		MaxTurns: 3,
	}
	result, err := agent.Run(ctx, userMsg, nil)
	if err != nil {
		return Verdict{}, fmt.Errorf("synthesis agent: %w", err)
	}

	v := Verdict{
		Target:        in.Target,
		Claims:        in.Claims,
		ThreatModel:   in.ThreatModel,
		Findings:      convertFindings(in.Findings),
		OpenQuestions: in.OpenQuestions,
		AuditMarkdown: result.Text,
		Model:         in.Model,
		PromptVersion: prompts.Version,
		Verdict:       ComputeVerdict(in.Findings),
	}
	return v, nil
}

// ComputeVerdict implements the deterministic verdict rule from synthesis.md.
//
//	any critical → unsafe
//	any high     → unsafe
//	any medium   → caution
//	≥3 low/info  → caution
//	otherwise    → safe
func ComputeVerdict(findings []tools.Finding) string {
	var critical, high, medium, lowInfo int
	for _, f := range findings {
		switch f.Severity {
		case "critical":
			critical++
		case "high":
			high++
		case "medium":
			medium++
		case "low", "info":
			lowInfo++
		}
	}
	switch {
	case critical > 0 || high > 0:
		return "unsafe"
	case medium > 0:
		return "caution"
	case lowInfo >= 3:
		return "caution"
	default:
		return "safe"
	}
}

// convertFindings transforms tools.Finding into FindingOut for the verdict.
func convertFindings(in []tools.Finding) []FindingOut {
	out := make([]FindingOut, 0, len(in))
	for i, f := range in {
		fo := FindingOut{
			ID:              fmt.Sprintf("F%d", i+1),
			Severity:        f.Severity,
			Category:        f.Category,
			Title:           f.Title,
			Description:     f.Description,
			ExploitScenario: f.ExploitScenario,
			ThreatID:        f.ThreatID,
		}
		for _, e := range f.Evidence {
			fo.Evidence = append(fo.Evidence, EvidenceOut{File: e.File, Line: e.Line, Snippet: e.Snippet})
		}
		out = append(out, fo)
	}
	return out
}
