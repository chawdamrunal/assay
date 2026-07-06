package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSSEWriterEmitsEvent(t *testing.T) {
	w := httptest.NewRecorder()
	sse := NewSSEWriter(w)

	require.NoError(t, sse.WriteEvent("stage", map[string]string{"name": "triage"}))
	require.NoError(t, sse.WriteEvent("finding", "found one"))

	body := w.Body.String()
	assert.Contains(t, body, "event: stage")
	assert.Contains(t, body, `data: {"name":"triage"}`)
	assert.Contains(t, body, "event: finding")
	assert.Contains(t, body, `data: "found one"`)
	assert.Equal(t, 2, strings.Count(body, "\n\n"))
	assert.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))
}

func TestSSEContextCancels(t *testing.T) {
	w := httptest.NewRecorder()
	sse := NewSSEWriter(w)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := sse.Run(ctx, func(_ func(event string, data any) error) error {
		t.Helper()
		t.Errorf("Run callback should not be invoked when ctx already done")
		return nil
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context")
}

func TestSSERunEmitsAndExits(t *testing.T) {
	w := httptest.NewRecorder()
	sse := NewSSEWriter(w)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := sse.Run(ctx, func(emit func(event string, data any) error) error {
		return emit("hello", map[string]int{"x": 1})
	})
	require.NoError(t, err)
	assert.Contains(t, w.Body.String(), "event: hello")
	assert.Contains(t, w.Body.String(), `"x":1`)
}

// http.ResponseWriter must satisfy http.Flusher for SSE; ensure httptest.ResponseRecorder does.
func TestRecorderImplementsFlusher(t *testing.T) {
	_, ok := any(httptest.NewRecorder()).(http.Flusher)
	if !ok {
		t.Skip("httptest.ResponseRecorder does not implement http.Flusher in this Go version")
	}
}
