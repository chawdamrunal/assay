package claude

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentRunSingleTurn(t *testing.T) {
	fc := NewFakeClient()
	fc.Enqueue(Response{Text: "hello world", Stop: "end_turn"})

	a := &Agent{
		Client: fc,
		Model:  "claude-sonnet-4-6",
		System: "be brief",
	}
	out, err := a.Run(context.Background(), "say hello", nil)
	require.NoError(t, err)
	assert.Equal(t, "hello world", out.Text)
	assert.Equal(t, 1, out.Turns)
}

func TestAgentRunToolLoop(t *testing.T) {
	fc := NewFakeClient()
	// Turn 1: agent calls read_file
	fc.Enqueue(Response{
		ToolUses: []ToolUse{{ID: "t1", Name: "read_file", Input: map[string]any{"path": "main.go"}}},
		Stop:     "tool_use",
	})
	// Turn 2: agent calls record_finding
	fc.Enqueue(Response{
		ToolUses: []ToolUse{{ID: "t2", Name: "record_finding", Input: map[string]any{"severity": "high"}}},
		Stop:     "tool_use",
	})
	// Turn 3: agent ends
	fc.Enqueue(Response{Text: "all done", Stop: "end_turn"})

	executed := []string{}
	tools := map[string]ToolHandler{
		"read_file": func(_ context.Context, _ ToolUse) (string, error) {
			executed = append(executed, "read_file")
			return "file contents here", nil
		},
		"record_finding": func(_ context.Context, _ ToolUse) (string, error) {
			executed = append(executed, "record_finding")
			return "ok", nil
		},
	}

	a := &Agent{Client: fc, Model: "claude-sonnet-4-6", Tools: tools}
	out, err := a.Run(context.Background(), "go", nil)
	require.NoError(t, err)
	assert.Equal(t, "all done", out.Text)
	assert.Equal(t, []string{"read_file", "record_finding"}, executed)
	assert.Equal(t, 3, len(fc.Requests()))
}

// TestAgentReconstructsAssistantTurnBeforeToolResult regression-guards QA-T8:
// after a tool_use turn the loop must append an assistant turn carrying the
// tool_use blocks BEFORE the user turn carrying the tool_result blocks, so the
// transcript alternates user/assistant/user and every tool_result references a
// tool_use_id present in the preceding assistant turn. The pre-fix code sent
// two consecutive user turns, producing 400 "unexpected tool_use_id in
// tool_result blocks" on the real API.
func TestAgentReconstructsAssistantTurnBeforeToolResult(t *testing.T) {
	fc := NewFakeClient()
	fc.Enqueue(Response{
		Text:     "let me read that file",
		ToolUses: []ToolUse{{ID: "tu-1", Name: "read_file", Input: map[string]any{"path": "main.go"}}},
		Stop:     "tool_use",
	})
	fc.Enqueue(Response{Text: "done", Stop: "end_turn"})

	tools := map[string]ToolHandler{
		"read_file": func(_ context.Context, _ ToolUse) (string, error) { return "contents", nil },
	}
	a := &Agent{Client: fc, Model: "m", Tools: tools}
	_, err := a.Run(context.Background(), "go", nil)
	require.NoError(t, err)

	reqs := fc.Requests()
	require.Len(t, reqs, 2)
	msgs := reqs[1].Messages
	require.Len(t, msgs, 3, "expected user/assistant/user, got %d messages", len(msgs))
	assert.Equal(t, "user", msgs[0].Role)
	assert.Equal(t, "assistant", msgs[1].Role)
	assert.Equal(t, "user", msgs[2].Role)

	// No two consecutive user turns — that is the exact QA-T8 failure shape.
	for i := 1; i < len(msgs); i++ {
		if msgs[i-1].Role == "user" && msgs[i].Role == "user" {
			t.Fatalf("two consecutive user turns at index %d-%d (QA-T8 regression)", i-1, i)
		}
	}

	// The assistant turn must carry a tool_use block whose ID + name + input
	// match what the model requested.
	var toolUseID string
	for _, c := range msgs[1].Content {
		if c.Type == "tool_use" {
			toolUseID = c.ToolUseID
			assert.Equal(t, "read_file", c.Name)
			assert.Equal(t, "main.go", c.Input["path"])
		}
	}
	require.NotEmpty(t, toolUseID, "assistant turn must contain a tool_use block")

	// The following user turn's tool_result must reference that exact ID.
	var resultID string
	for _, c := range msgs[2].Content {
		if c.Type == "tool_result" {
			resultID = c.ToolUseID
		}
	}
	assert.Equal(t, toolUseID, resultID, "tool_result must reference the assistant's tool_use ID")
}

func TestAgentRespectsMaxTurns(t *testing.T) {
	fc := NewFakeClient()
	for i := 0; i < 50; i++ {
		fc.Enqueue(Response{
			ToolUses: []ToolUse{{ID: "loop", Name: "read_file"}},
			Stop:     "tool_use",
		})
	}
	tools := map[string]ToolHandler{
		"read_file": func(_ context.Context, _ ToolUse) (string, error) { return "x", nil },
	}
	a := &Agent{Client: fc, Model: "m", Tools: tools, MaxTurns: 5}

	_, err := a.Run(context.Background(), "go", nil)
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "max turns")
}

func TestAgentToolErrorIsReportedToModel(t *testing.T) {
	fc := NewFakeClient()
	fc.Enqueue(Response{
		ToolUses: []ToolUse{{ID: "t1", Name: "bad_tool"}},
		Stop:     "tool_use",
	})
	fc.Enqueue(Response{Text: "recovered", Stop: "end_turn"})

	tools := map[string]ToolHandler{
		"bad_tool": func(_ context.Context, _ ToolUse) (string, error) {
			return "", errors.New("boom")
		},
	}
	a := &Agent{Client: fc, Model: "m", Tools: tools}
	out, err := a.Run(context.Background(), "go", nil)
	require.NoError(t, err)
	assert.Equal(t, "recovered", out.Text)

	// The 2nd request should have a user message with the tool error.
	reqs := fc.Requests()
	require.Len(t, reqs, 2)
	last := reqs[1].Messages[len(reqs[1].Messages)-1]
	require.Equal(t, "user", last.Role)
	require.NotEmpty(t, last.Content)
	assert.True(t, last.Content[0].IsError, "tool error should be marked is_error")
}

func TestAgentUnknownToolReturnsErrorToModel(t *testing.T) {
	fc := NewFakeClient()
	fc.Enqueue(Response{
		ToolUses: []ToolUse{{ID: "t1", Name: "no_such_tool"}},
		Stop:     "tool_use",
	})
	fc.Enqueue(Response{Text: "recovered", Stop: "end_turn"})

	a := &Agent{Client: fc, Model: "m", Tools: map[string]ToolHandler{}}
	_, err := a.Run(context.Background(), "go", nil)
	require.NoError(t, err)

	reqs := fc.Requests()
	require.Len(t, reqs, 2)
	last := reqs[1].Messages[len(reqs[1].Messages)-1]
	assert.True(t, last.Content[0].IsError)
	assert.Contains(t, strings.ToLower(last.Content[0].Text), "unknown tool")
}

func TestAgentNilClient(t *testing.T) {
	a := &Agent{Model: "m"}
	_, err := a.Run(context.Background(), "go", nil)
	require.Error(t, err)
}
