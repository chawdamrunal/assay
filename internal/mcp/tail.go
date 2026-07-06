package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"
)

// TailEvents tails a scan's events.jsonl, emitting each new line as a parsed
// ProgressEvent on the returned channel. The channel closes when the context
// is canceled OR when a terminal {stage:"done"} event is observed.
//
// This is how the HTTP SSE bridge follows scans driven by an out-of-process
// Claude Code subprocess: the subprocess writes events via assay_emit_progress
// (which writes to events.jsonl), and this routine pumps them back into the
// in-memory runner so /api/scans/:id/stream subscribers see live progress
// without caring about the underlying transport.
func TailEvents(ctx context.Context, scanDir string) <-chan ProgressEvent {
	out := make(chan ProgressEvent, 32)
	go func() {
		defer close(out)
		path := filepath.Join(scanDir, "events.jsonl")
		var (
			f       *os.File
			scanner *bufio.Scanner
		)
		// Open lazily: events.jsonl is created by assay_scan_start which runs
		// AFTER this tailer starts. Poll until it exists, then follow.
		for f == nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(150 * time.Millisecond):
			}
			openF, err := os.Open(path) // #nosec G304 -- scanDir is allocator-bound
			if err == nil {
				f = openF
				scanner = bufio.NewScanner(f)
				scanner.Buffer(make([]byte, 64*1024), 1024*1024)
				continue
			}
			if !errors.Is(err, os.ErrNotExist) {
				// Real error reading the file → bail (caller treats channel
				// close as "stream done").
				return
			}
		}
		defer func() { _ = f.Close() }()

		// Follow loop: read available lines, sleep briefly, repeat.
		for {
			for scanner.Scan() {
				line := scanner.Bytes()
				if len(line) == 0 {
					continue
				}
				var ev ProgressEvent
				if err := json.Unmarshal(line, &ev); err != nil {
					continue
				}
				select {
				case out <- ev:
				case <-ctx.Done():
					return
				}
				if ev.Stage == "done" {
					return
				}
			}
			if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
				return
			}
			// Reset the scanner so it sees the newly-appended data on next round.
			select {
			case <-ctx.Done():
				return
			case <-time.After(150 * time.Millisecond):
			}
			scanner = bufio.NewScanner(f)
			scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		}
	}()
	return out
}
