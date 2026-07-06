package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// SSEWriter wraps an http.ResponseWriter for Server-Sent Events.
// The first WriteEvent call sets the required headers; subsequent calls
// emit "event: <name>\ndata: <json>\n\n" frames and flush.
type SSEWriter struct {
	w        http.ResponseWriter
	flusher  http.Flusher
	prepared bool
}

// NewSSEWriter constructs an SSEWriter. The underlying ResponseWriter must
// also implement http.Flusher; if not, WriteEvent returns an error.
func NewSSEWriter(w http.ResponseWriter) *SSEWriter {
	f, _ := w.(http.Flusher)
	return &SSEWriter{w: w, flusher: f}
}

// WriteEvent emits a single SSE frame and flushes.
func (s *SSEWriter) WriteEvent(event string, data any) error {
	if s.flusher == nil {
		return errors.New("response writer does not support flushing")
	}
	if !s.prepared {
		s.w.Header().Set("Content-Type", "text/event-stream")
		s.w.Header().Set("Cache-Control", "no-cache")
		s.w.Header().Set("Connection", "keep-alive")
		s.prepared = true
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal sse data: %w", err)
	}
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, payload); err != nil {
		return fmt.Errorf("write sse: %w", err)
	}
	s.flusher.Flush()
	return nil
}

// Run executes work, passing an emit callback. If ctx is already done it
// returns immediately with the ctx error before invoking work.
func (s *SSEWriter) Run(ctx context.Context, work func(emit func(event string, data any) error) error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context: %w", err)
	}
	return work(func(event string, data any) error {
		return s.WriteEvent(event, data)
	})
}
