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

// TriageInput holds the dependencies and inputs for Stage 0.
type TriageInput struct {
	Client       claude.Client
	Model        string
	Target       string
	Prepass      prepass.Result
	ToolDefs     []claude.ToolDef
	ToolHandlers map[string]claude.ToolHandler
}

// RunTriage executes Stage 0 — produce a TriageMap for the target.
func RunTriage(ctx context.Context, in TriageInput) (TriageMap, error) {
	system, err := prompts.Load(prompts.Version, "triage")
	if err != nil {
		return TriageMap{}, fmt.Errorf("triage: load prompt: %w", err)
	}

	prepassJSON, _ := json.Marshal(in.Prepass)
	userMsg := fmt.Sprintf("Target: %s\n\nPre-pass evidence:\n```json\n%s\n```\n\nRun triage now.", in.Target, string(prepassJSON))

	agent := &claude.Agent{
		Client:   in.Client,
		Model:    in.Model,
		System:   system,
		Tools:    in.ToolHandlers,
		ToolDefs: in.ToolDefs,
		MaxTurns: 8,
	}

	result, err := agent.Run(ctx, userMsg, nil)
	if err != nil {
		return TriageMap{}, fmt.Errorf("triage agent: %w", err)
	}

	var triage TriageMap
	if err := unmarshalJSONBody(result.Text, &triage); err != nil {
		return TriageMap{}, fmt.Errorf("triage: parse output: %w (output was: %s)", err, truncate(result.Text, 300))
	}
	return triage, nil
}

// unmarshalJSONBody extracts a JSON object from text that may be wrapped in
// markdown code fences or surrounded by prose, and decodes it into v.
var jsonFenceRe = regexp.MustCompile("(?s)```(?:json)?\\s*\\n(.*?)\\n```")

func unmarshalJSONBody(text string, v any) error {
	body := strings.TrimSpace(text)
	if m := jsonFenceRe.FindStringSubmatch(body); m != nil {
		body = strings.TrimSpace(m[1])
	}
	// Find the bounds of a JSON object or array.
	if i := strings.IndexByte(body, '{'); i >= 0 {
		if j := strings.LastIndexByte(body, '}'); j > i {
			objBody := body[i : j+1]
			if err := json.Unmarshal([]byte(objBody), v); err == nil {
				return nil
			}
		}
	}
	if i := strings.IndexByte(body, '['); i >= 0 {
		if j := strings.LastIndexByte(body, ']'); j > i {
			arrBody := body[i : j+1]
			if err := json.Unmarshal([]byte(arrBody), v); err == nil {
				return nil
			}
		}
	}
	return json.Unmarshal([]byte(body), v)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
