// Package prepass runs cheap, deterministic checks on a target before the
// Sonnet-driven scanner sees it. Its output is fed to Stage 0 (triage) as
// starting evidence, never as final verdicts.
package prepass

import "time"

// Hit is a single deterministic finding (secret, suspicious pattern, CVE match).
// Hits are evidence for the LLM agent, not verdicts. The agent decides what matters.
type Hit struct {
	Category string            `json:"category"` // "secret" | "pattern" | "cve"
	Severity string            `json:"severity"` // "critical" | "high" | "medium" | "low" | "info"
	File     string            `json:"file,omitempty"`
	Line     int               `json:"line,omitempty"`
	Snippet  string            `json:"snippet,omitempty"` // verbatim, never reconstructed
	Message  string            `json:"message"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Result is the full prepass output for one target.
type Result struct {
	Target    string    `json:"target"`
	RanAt     time.Time `json:"ran_at"`
	Hits      []Hit     `json:"hits"`
	Manifests []string  `json:"manifests,omitempty"` // discovered manifest paths
}

// Options configures a prepass run.
type Options struct {
	// Offline disables network-dependent checks (OSV lookups).
	Offline bool
	// MaxFileSize skips files larger than this many bytes when scanning content.
	// Default 1 MiB; oversized files are flagged but not opened.
	MaxFileSize int64
}

// DefaultMaxFileSize is the byte cap when Options.MaxFileSize is zero.
const DefaultMaxFileSize = 1 << 20 // 1 MiB
