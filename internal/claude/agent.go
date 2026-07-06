package claude

import (
	"context"
	"errors"
	"fmt"
)

// ToolHandler executes one tool invocation and returns the result text the
// model should see. If the handler returns an error, the loop sends the
// error message back to the model with is_error=true so it can recover.
type ToolHandler func(ctx context.Context, use ToolUse) (string, error)

// Agent drives a single Sonnet conversation through tool_use turns until end_turn.
type Agent struct {
	Client   Client
	Model    string
	System   string
	Tools    map[string]ToolHandler
	ToolDefs []ToolDef
	MaxTurns int // 0 = default 20
}

// Result captures the agent's final output and aggregate usage.
type Result struct {
	Text     string
	Turns    int
	Usage    Usage
	Messages []Message // full transcript for investigation.log
}

// Run executes the agent loop. The userPrompt becomes the first user message;
// priorMessages (optional) prepends an existing conversation history.
func (a *Agent) Run(ctx context.Context, userPrompt string, priorMessages []Message) (Result, error) {
	maxTurns := a.MaxTurns
	if maxTurns == 0 {
		maxTurns = 20
	}
	if a.Client == nil {
		return Result{}, errors.New("agent: Client is nil")
	}
	if a.Tools == nil {
		a.Tools = map[string]ToolHandler{}
	}

	messages := append([]Message{}, priorMessages...)
	messages = append(messages, Message{
		Role:    "user",
		Content: []Content{{Type: "text", Text: userPrompt}},
	})

	var aggUsage Usage
	for turn := 1; turn <= maxTurns; turn++ {
		resp, err := a.Client.Complete(ctx, Request{
			Model:    a.Model,
			System:   a.System,
			Messages: messages,
			Tools:    a.ToolDefs,
		})
		if err != nil {
			return Result{}, fmt.Errorf("agent turn %d: %w", turn, err)
		}
		aggUsage.InputTokens += resp.Usage.InputTokens
		aggUsage.OutputTokens += resp.Usage.OutputTokens
		aggUsage.CacheCreationTokens += resp.Usage.CacheCreationTokens
		aggUsage.CacheReadTokens += resp.Usage.CacheReadTokens

		if resp.Stop != "tool_use" {
			return Result{
				Text:     resp.Text,
				Turns:    turn,
				Usage:    aggUsage,
				Messages: messages,
			}, nil
		}

		// Execute each tool, then send results as a single user turn with tool_result blocks.
		toolResults := make([]Content, 0, len(resp.ToolUses))
		for _, tu := range resp.ToolUses {
			handler, ok := a.Tools[tu.Name]
			if !ok {
				toolResults = append(toolResults, Content{
					Type:      "tool_result",
					ToolUseID: tu.ID,
					Text:      "unknown tool: " + tu.Name,
					IsError:   true,
				})
				continue
			}
			out, err := handler(ctx, tu)
			if err != nil {
				toolResults = append(toolResults, Content{
					Type:      "tool_result",
					ToolUseID: tu.ID,
					Text:      err.Error(),
					IsError:   true,
				})
				continue
			}
			toolResults = append(toolResults, Content{
				Type:      "tool_result",
				ToolUseID: tu.ID,
				Text:      out,
			})
		}
		// Reconstruct the assistant turn that requested these tools BEFORE the
		// user turn carrying their results. The Anthropic API requires every
		// tool_result's tool_use_id to match a tool_use block in the
		// immediately-preceding assistant turn; appending the tool_result
		// user turn after the original user prompt (without this assistant
		// turn) produces 400 "unexpected tool_use_id in tool_result blocks"
		// (regression: QA-T8).
		assistantContent := make([]Content, 0, len(resp.ToolUses)+1)
		if resp.Text != "" {
			assistantContent = append(assistantContent, Content{Type: "text", Text: resp.Text})
		}
		for _, tu := range resp.ToolUses {
			assistantContent = append(assistantContent, Content{
				Type:      "tool_use",
				ToolUseID: tu.ID,
				Name:      tu.Name,
				Input:     tu.Input,
			})
		}
		messages = append(messages, Message{Role: "assistant", Content: assistantContent})
		messages = append(messages, Message{Role: "user", Content: toolResults})
	}

	return Result{Usage: aggUsage}, fmt.Errorf("agent: max turns (%d) exceeded", maxTurns)
}
