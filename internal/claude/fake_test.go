package claude

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFakeClientCapturesRequest(t *testing.T) {
	fc := NewFakeClient()
	fc.Enqueue(Response{Text: "ok", Stop: "end_turn"})

	_, err := fc.Complete(context.Background(), Request{
		Model:  "claude-sonnet-4-6",
		System: "system A",
	})
	require.NoError(t, err)

	require.Len(t, fc.Requests(), 1)
	assert.Equal(t, "system A", fc.Requests()[0].System)
}

func TestFakeClientToolUseFlow(t *testing.T) {
	fc := NewFakeClient()
	fc.Enqueue(Response{
		ToolUses: []ToolUse{{ID: "t1", Name: "read_file", Input: map[string]any{"path": "x"}}},
		Stop:     "tool_use",
	})
	fc.Enqueue(Response{Text: "done", Stop: "end_turn"})

	r1, err := fc.Complete(context.Background(), Request{})
	require.NoError(t, err)
	assert.Equal(t, "tool_use", r1.Stop)
	assert.Len(t, r1.ToolUses, 1)

	r2, err := fc.Complete(context.Background(), Request{})
	require.NoError(t, err)
	assert.Equal(t, "end_turn", r2.Stop)
	assert.Equal(t, "done", r2.Text)
}

func TestNewRealClientRejectsEmptyKey(t *testing.T) {
	_, err := NewRealClient("", nil)
	require.Error(t, err)
}
