package scanner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/chawdamrunal/assay/internal/claude"
	"github.com/chawdamrunal/assay/internal/prepass"
	"github.com/chawdamrunal/assay/internal/tools"
)

// DefaultModel is the concrete model the legacy/in-process scanner falls back
// to when no model is configured. Unlike MCP mode — where an empty model means
// "let Claude Code choose what the subscription allows" — the Anthropic
// Messages API this path calls requires a concrete model ID. Config defaults
// are "auto" (empty), so this substitution keeps API-key/CI scans working.
const DefaultModel = "claude-sonnet-4-6"

// Scan executes the full 5-stage scanner against opts.Target.
// Progress events are emitted to the optional events channel (nil = no events).
// The events channel is NOT closed by Scan; the caller owns it.
func Scan(ctx context.Context, opts Options, client claude.Client, events chan<- Event) (*Verdict, error) {
	// Legacy/API path needs a concrete model; "" (auto) is only meaningful in
	// MCP mode where Claude Code picks the model.
	if opts.Model == "" {
		opts.Model = DefaultModel
	}

	emit := func(stage, status, msg string) {
		if events == nil {
			return
		}
		select {
		case events <- Event{Stage: stage, Status: status, Message: msg}:
		default:
		}
	}

	// Pre-pass
	emit("prepass", "start", "")
	ppResult, err := prepass.Run(opts.Target, prepass.Options{Offline: opts.Offline})
	if err != nil {
		emit("prepass", "error", err.Error())
		return nil, fmt.Errorf("prepass: %w", err)
	}
	emit("prepass", "complete", fmt.Sprintf("%d hits", len(ppResult.Hits)))

	// Build tool layer scoped to the target.
	fsTool := tools.NewFS(opts.Target)
	manifestTool := tools.NewManifest(opts.Target)
	secretsTool := tools.NewSecretsTool(opts.Target)
	osvTool := tools.NewOSVTool(prepass.DefaultOSV())

	fsDefs := fsTool.Defs()
	subagentDefs := []claude.ToolDef{
		toClaudeDef(fsDefs[0]), // read_file
		toClaudeDef(fsDefs[1]), // list_dir
		toClaudeDef(fsDefs[2]), // grep
		toClaudeDef(fsDefs[3]), // symbol_refs
		toClaudeDef(manifestTool.Def()),
		toClaudeDef(secretsTool.Def()),
		toClaudeDef(osvTool.Def()),
		// record_finding is added by the dispatcher per sub-agent.
	}
	subagentHandlers := map[string]claude.ToolHandler{
		"read_file":      wrapToolHandler(fsTool.ReadFile),
		"list_dir":       wrapToolHandler(fsTool.ListDir),
		"grep":           wrapToolHandler(fsTool.Grep),
		"symbol_refs":    wrapToolHandler(fsTool.SymbolRefs),
		"parse_manifest": wrapToolHandler(manifestTool.Parse),
		"secret_scan":    wrapToolHandler(secretsTool.Scan),
		"osv_lookup":     wrapToolHandler(osvTool.Lookup),
	}

	// Stage 0
	emit("triage", "start", "")
	triage, err := RunTriage(ctx, TriageInput{
		Client:  client,
		Model:   opts.Model,
		Target:  opts.Target,
		Prepass: ppResult,
		// Stage 0 doesn't need the heavy tool set — only manifest parsing.
		ToolDefs:     []claude.ToolDef{toClaudeDef(manifestTool.Def())},
		ToolHandlers: map[string]claude.ToolHandler{"parse_manifest": wrapToolHandler(manifestTool.Parse)},
	})
	if err != nil {
		emit("triage", "error", err.Error())
		return nil, err
	}
	emit("triage", "complete", "")

	// Stage 1
	emit("claims", "start", "")
	readme := readREADME(opts.Target)
	claims, err := RunClaims(ctx, ClaimsInput{
		Client:     client,
		Model:      opts.Model,
		Triage:     triage,
		ReadmeText: readme,
	})
	if err != nil {
		emit("claims", "error", err.Error())
		return nil, err
	}
	emit("claims", "complete", "")

	// Stage 2
	emit("threat_model", "start", "")
	tm, err := RunThreatModel(ctx, ThreatModelInput{
		Client:  client,
		Model:   opts.Model,
		Triage:  triage,
		Claims:  claims,
		Prepass: ppResult,
	})
	if err != nil {
		emit("threat_model", "error", err.Error())
		return nil, err
	}
	emit("threat_model", "complete", fmt.Sprintf("%d threats", len(tm.Threats)))

	// Stage 3
	emit("investigation", "start", "")
	model := opts.ModelInvestigation
	if model == "" {
		model = opts.Model
	}
	findings, openQs, err := RunInvestigation(ctx, InvestigationInput{
		Client:              client,
		Model:               model,
		ThreatModel:         tm,
		MaxConcurrency:      opts.SubagentConcurrency,
		MaxTurnsPerSubagent: 20,
		SubagentDefs:        subagentDefs,
		SubagentHandlers:    subagentHandlers,
	})
	if err != nil {
		emit("investigation", "error", err.Error())
		return nil, err
	}
	emit("investigation", "complete", fmt.Sprintf("%d findings", len(findings)))

	// Stage 4
	emit("exploitability", "start", "")
	findings, err = RunExploitability(ctx, ExploitabilityInput{
		Client:   client,
		Model:    opts.Model,
		Findings: findings,
	})
	if err != nil {
		emit("exploitability", "error", err.Error())
		return nil, err
	}
	emit("exploitability", "complete", "")

	// Stage 5
	emit("synthesis", "start", "")
	v, err := RunSynthesis(ctx, SynthesisInput{
		Client:        client,
		Model:         opts.Model,
		Target:        opts.Target,
		Claims:        claims,
		ThreatModel:   tm,
		Findings:      findings,
		OpenQuestions: openQs,
	})
	if err != nil {
		emit("synthesis", "error", err.Error())
		return nil, err
	}
	v.ScanID = uuid.NewString()
	emit("synthesis", "complete", "")
	emit("done", "complete", v.Verdict)

	return &v, nil
}

func toClaudeDef(t tools.Tool) claude.ToolDef {
	return claude.ToolDef{
		Name:        t.Name,
		Description: t.Description,
		InputSchema: t.InputSchema,
	}
}

func wrapToolHandler(h func(context.Context, tools.Invocation) (tools.Result, error)) claude.ToolHandler {
	return func(ctx context.Context, use claude.ToolUse) (string, error) {
		r, err := h(ctx, tools.Invocation{Name: use.Name, Input: use.Input})
		if err != nil {
			return "", err
		}
		return r.Text, nil
	}
}

func readREADME(root string) string {
	for _, name := range []string{"README.md", "README", "readme.md"} {
		data, err := os.ReadFile(filepath.Join(root, name)) // #nosec G304 -- known filename under target root
		if err == nil {
			return string(data)
		}
	}
	return ""
}
