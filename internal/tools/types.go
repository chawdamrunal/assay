// Package tools provides the bounded set of operations the scanner exposes
// to Sonnet via tool_use. Every tool operates relative to a configured root
// (the target being scanned). Paths that escape the root return an error.
package tools

import "context"

// Tool is the agent-facing description of a tool.
type Tool struct {
	Name        string
	Description string
	InputSchema map[string]any
}

// Invocation is one call from the model.
type Invocation struct {
	Name  string
	Input map[string]any
}

// Result is the text returned to the model after a tool call.
type Result struct {
	Text string
}

// Handler is the function signature for executing a tool call.
type Handler func(ctx context.Context, in Invocation) (Result, error)
