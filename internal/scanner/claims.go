package scanner

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/chawdamrunal/assay/internal/claude"
	"github.com/chawdamrunal/assay/internal/prompts"
)

// ClaimsInput holds the dependencies and inputs for Stage 1.
type ClaimsInput struct {
	Client       claude.Client
	Model        string
	Triage       TriageMap
	ReadmeText   string // raw README text, if any
	ManifestJSON string // JSON-encoded manifest, if any
	ToolDefs     []claude.ToolDef
	ToolHandlers map[string]claude.ToolHandler
}

// RunClaims executes Stage 1 — extract what the artifact CLAIMS to do.
func RunClaims(ctx context.Context, in ClaimsInput) (Claims, error) {
	system, err := prompts.Load(prompts.Version, "claims")
	if err != nil {
		return Claims{}, fmt.Errorf("claims: load prompt: %w", err)
	}

	triageJSON, _ := json.Marshal(in.Triage)
	userMsg := fmt.Sprintf(`Triage map:
%s

README text:
%s

Manifest (parsed):
%s

Extract the artifact's claims now.`, string(triageJSON), in.ReadmeText, in.ManifestJSON)

	agent := &claude.Agent{
		Client:   in.Client,
		Model:    in.Model,
		System:   system,
		Tools:    in.ToolHandlers,
		ToolDefs: in.ToolDefs,
		MaxTurns: 5,
	}
	result, err := agent.Run(ctx, userMsg, nil)
	if err != nil {
		return Claims{}, fmt.Errorf("claims agent: %w", err)
	}

	var claims Claims
	if err := unmarshalJSONBody(result.Text, &claims); err != nil {
		return Claims{}, fmt.Errorf("claims: parse output: %w (output was: %s)", err, truncate(result.Text, 300))
	}
	return claims, nil
}
