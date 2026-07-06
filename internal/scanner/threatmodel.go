package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/chawdamrunal/assay/internal/claude"
	"github.com/chawdamrunal/assay/internal/prepass"
	"github.com/chawdamrunal/assay/internal/prompts"
)

// ThreatModelInput holds dependencies for Stage 2.
type ThreatModelInput struct {
	Client       claude.Client
	Model        string
	Triage       TriageMap
	Claims       Claims
	Prepass      prepass.Result
	ToolDefs     []claude.ToolDef
	ToolHandlers map[string]claude.ToolHandler
}

// RunThreatModel executes Stage 2 — produce a ThreatModel from claims + declared capabilities.
func RunThreatModel(ctx context.Context, in ThreatModelInput) (ThreatModel, error) {
	system, err := prompts.Load(prompts.Version, "threat_model")
	if err != nil {
		return ThreatModel{}, fmt.Errorf("threat_model: load prompt: %w", err)
	}

	triageJSON, _ := json.Marshal(in.Triage)
	claimsJSON, _ := json.Marshal(in.Claims)
	prepassJSON, _ := json.Marshal(in.Prepass)
	userMsg := fmt.Sprintf(`Triage map:
%s

Claims:
%s

Pre-pass evidence:
%s

Produce the threat model now.`, string(triageJSON), string(claimsJSON), string(prepassJSON))

	agent := &claude.Agent{
		Client:   in.Client,
		Model:    in.Model,
		System:   system,
		Tools:    in.ToolHandlers,
		ToolDefs: in.ToolDefs,
		MaxTurns: 3, // threat model should not need tools; small budget
	}
	result, err := agent.Run(ctx, userMsg, nil)
	if err != nil {
		return ThreatModel{}, fmt.Errorf("threat_model agent: %w", err)
	}

	tm := ThreatModel{
		RawMarkdown: result.Text,
		Threats:     parseThreats(result.Text),
	}
	return tm, nil
}

// threatHeaderRe matches "### T<n>: <title>"
var threatHeaderRe = regexp.MustCompile(`(?m)^###\s+(T\d+):\s+(.+?)\s*$`)

// keyValueRe matches "**Key:** value" on one line
var keyValueRe = regexp.MustCompile(`(?m)^\*\*([^*]+):\*\*\s*(.+?)\s*$`)

// parseThreats walks the markdown and extracts Threat records.
func parseThreats(md string) []Threat {
	headerMatches := threatHeaderRe.FindAllStringSubmatchIndex(md, -1)
	if len(headerMatches) == 0 {
		return nil
	}

	var threats []Threat
	for i, hm := range headerMatches {
		idStart, idEnd := hm[2], hm[3]
		titleStart, titleEnd := hm[4], hm[5]
		// The block runs from end-of-header to start-of-next-header (or EOF).
		blockStart := hm[1]
		blockEnd := len(md)
		if i+1 < len(headerMatches) {
			blockEnd = headerMatches[i+1][0]
		}
		block := md[blockStart:blockEnd]

		t := Threat{
			ID:    md[idStart:idEnd],
			Title: md[titleStart:titleEnd],
		}

		// Pull out **Class:**, **Severity:** / **Severity if exploited:**, **Description:**.
		kvMatches := keyValueRe.FindAllStringSubmatch(block, -1)
		for _, m := range kvMatches {
			key := strings.ToLower(strings.TrimSpace(m[1]))
			val := strings.TrimSpace(m[2])
			switch key {
			case "class":
				t.Class = val
			case "severity", "severity if exploited":
				t.Severity = strings.ToLower(val)
			case "description":
				t.Description = val
			}
		}

		// Reviewer questions: bullet list after "**Reviewer questions:**".
		if idx := strings.Index(strings.ToLower(block), "**reviewer questions:**"); idx >= 0 {
			tail := block[idx:]
			lines := strings.Split(tail, "\n")
			for j := 1; j < len(lines); j++ {
				line := strings.TrimSpace(lines[j])
				if line == "" {
					continue
				}
				if strings.HasPrefix(line, "- ") {
					t.ReviewerQuestions = append(t.ReviewerQuestions, strings.TrimSpace(line[2:]))
				} else if !strings.HasPrefix(line, "*") && !strings.HasPrefix(line, "#") {
					break
				}
			}
		}

		threats = append(threats, t)
	}
	return threats
}
