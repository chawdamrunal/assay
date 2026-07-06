package tools

import (
	"context"
	"errors"
	"fmt"

	"github.com/chawdamrunal/assay/internal/claude"
)

// DispatcherConfig holds shared dependencies for sub-agent dispatch.
type DispatcherConfig struct {
	Client         claude.Client
	Model          string
	System         string // the investigator prompt (loaded from internal/prompts)
	MaxConcurrency int    // semaphore size; default 3
	MaxTurns       int    // per sub-agent; default 20
	ParentFindings *Findings
	// SubagentTools provides the per-threat tool handlers and definitions
	// (read_file, list_dir, grep, record_finding, parse_manifest, etc.).
	SubagentTools    []Tool
	SubagentDefs     []claude.ToolDef
	SubagentHandlers map[string]claude.ToolHandler
}

// Dispatcher runs investigator sub-agents on demand.
type Dispatcher struct {
	cfg DispatcherConfig
	sem chan struct{}
}

// NewDispatcher returns a Dispatcher.
func NewDispatcher(cfg DispatcherConfig) *Dispatcher {
	if cfg.MaxConcurrency <= 0 {
		cfg.MaxConcurrency = 3
	}
	if cfg.MaxTurns <= 0 {
		cfg.MaxTurns = 20
	}
	return &Dispatcher{
		cfg: cfg,
		sem: make(chan struct{}, cfg.MaxConcurrency),
	}
}

// Def returns the agent-facing tool definition.
func (d *Dispatcher) Def() Tool {
	return Tool{
		Name:        "dispatch_subagent",
		Description: "Spawn a focused investigator sub-agent to examine ONE specific threat from the threat model. Returns a summary of the sub-agent's findings (recorded in the shared findings collector).",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"threat_id":          map[string]any{"type": "string"},
				"threat_title":       map[string]any{"type": "string"},
				"threat_description": map[string]any{"type": "string"},
				"reviewer_questions": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
			"required": []string{"threat_id", "threat_title", "threat_description"},
		},
	}
}

// Dispatch runs one investigator sub-agent for one threat.
func (d *Dispatcher) Dispatch(ctx context.Context, in Invocation) (Result, error) {
	threatID, _ := in.Input["threat_id"].(string)
	if threatID == "" {
		return Result{}, errors.New("dispatch_subagent: threat_id is required")
	}
	threatTitle, _ := in.Input["threat_title"].(string)
	threatDesc, _ := in.Input["threat_description"].(string)
	if threatTitle == "" || threatDesc == "" {
		return Result{}, errors.New("dispatch_subagent: threat_title and threat_description are required")
	}
	questions, _ := in.Input["reviewer_questions"].([]any)
	var qStr string
	for _, q := range questions {
		if s, ok := q.(string); ok {
			qStr += "- " + s + "\n"
		}
	}

	// Acquire a slot.
	select {
	case d.sem <- struct{}{}:
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
	defer func() { <-d.sem }()

	// Each sub-agent writes to its own Findings collector with threat_id stamped;
	// after run, we MergeAll back into the parent.
	subCollector := d.cfg.ParentFindings.WithThreatID(threatID)

	// Override the record_finding handler to write to subCollector.
	handlers := make(map[string]claude.ToolHandler, len(d.cfg.SubagentHandlers))
	for k, v := range d.cfg.SubagentHandlers {
		handlers[k] = v
	}
	handlers["record_finding"] = func(_ context.Context, use claude.ToolUse) (string, error) {
		r, err := subCollector.Record(ctx, Invocation{Name: "record_finding", Input: use.Input})
		if err != nil {
			return "", err
		}
		return r.Text, nil
	}

	prompt := fmt.Sprintf(`Threat to investigate:
  ID: %s
  Title: %s
  Description: %s
  Reviewer questions:
%s

Investigate this threat using the available tools. Record findings via record_finding (use severity=info with empty evidence to report "no issues found"). Stay focused on THIS threat only.`,
		threatID, threatTitle, threatDesc, qStr)

	agent := &claude.Agent{
		Client:   d.cfg.Client,
		Model:    d.cfg.Model,
		System:   d.cfg.System,
		Tools:    handlers,
		ToolDefs: d.cfg.SubagentDefs,
		MaxTurns: d.cfg.MaxTurns,
	}

	_, err := agent.Run(ctx, prompt, nil)
	if err != nil {
		return Result{}, fmt.Errorf("dispatch_subagent[%s]: %w", threatID, err)
	}

	// Merge sub-agent findings into parent.
	d.cfg.ParentFindings.MergeAll(subCollector)

	count := len(subCollector.All())
	return Result{Text: fmt.Sprintf("sub-agent for threat %s completed with %d finding(s)", threatID, count)}, nil
}
