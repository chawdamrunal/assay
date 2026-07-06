package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/chawdamrunal/assay/internal/prepass"
)

// QuickResult is the v0.4 tier-1 output produced by `assay scan --quick`.
// It carries enough signal for the pre-install gate to decide allow/ask/deny
// without an LLM call — the deep scan runs separately in the background.
//
// Risk values:
//   - "low"      — no concerning pre-pass hits; safe to proceed
//   - "medium"   — pattern hits at info/low level (e.g. plain `fetch` calls)
//   - "high"     — pattern hits flagged high severity (eval, child_process,
//     fs read of .aws/.ssh/.env, base64 blobs)
//   - "critical" — high-severity pattern hit AND a secret was scanned in,
//     OR multiple high-severity hits clustering at one entry
//     (indicates active exfiltration intent, not just dual-use)
type QuickResult struct {
	Target    string        `json:"target"`
	RanAt     time.Time     `json:"ran_at"`
	Risk      string        `json:"risk"`
	Hits      []prepass.Hit `json:"hits"`
	Manifests []string      `json:"manifests,omitempty"`
	// Counts is a small histogram a JSON consumer (the hook script) can
	// summarize without rescoring our heuristic.
	Counts struct {
		Critical int `json:"critical"`
		High     int `json:"high"`
		Medium   int `json:"medium"`
		Low      int `json:"low"`
		Info     int `json:"info"`
		Secrets  int `json:"secrets"`
	} `json:"counts"`
	// DeepScanID, when set, is the UUID of the background full scan that the
	// caller can poll later for the citation-verified verdict.
	DeepScanID string `json:"deep_scan_id,omitempty"`
}

// RunQuick executes the v0.4 tier-1 profile against root. It is deterministic
// (no LLM call), targets <2s on a 30-file plugin, and is safe to call from a
// blocking Claude Code hook with a tight timeout.
//
// The risk score below is intentionally conservative: a "critical" decision
// blocks a `/plugin install`, so we err on the side of asking when uncertain.
func RunQuick(ctx context.Context, root string) (*QuickResult, error) {
	_ = ctx // pre-pass is filesystem-bounded; cancellation handled by callers via short timeout

	pp, err := prepass.Run(root, prepass.Options{Offline: true})
	if err != nil {
		return nil, fmt.Errorf("prepass: %w", err)
	}

	out := &QuickResult{
		Target:    root,
		RanAt:     time.Now().UTC(),
		Hits:      pp.Hits,
		Manifests: pp.Manifests,
	}
	for _, h := range pp.Hits {
		switch h.Severity {
		case "critical":
			out.Counts.Critical++
		case "high":
			out.Counts.High++
		case "medium":
			out.Counts.Medium++
		case "low":
			out.Counts.Low++
		case "info":
			out.Counts.Info++
		}
		if h.Category == "secret" {
			out.Counts.Secrets++
		}
	}
	out.Risk = scoreRisk(out)
	return out, nil
}

// scoreRisk applies the heuristic. Conservative ordering:
//
//  1. Any pre-pass hit flagged "critical" (e.g. AWS private-key regex match)
//     → critical regardless of other signals.
//  2. ≥1 secret-category hit AND ≥1 high-severity pattern → critical (the
//     pairing is the textbook exfil shape).
//  3. ≥1 high-severity pattern hit → high (e.g. fs.readFile on `.aws/`,
//     child_process spawn).
//  4. ≥2 medium pattern hits → medium (clustering is suspicious; one alone
//     in cosmetic code is usually fine).
//  5. Anything else (only info/low) → low.
func scoreRisk(r *QuickResult) string {
	switch {
	case r.Counts.Critical > 0:
		return "critical"
	case r.Counts.Secrets > 0 && r.Counts.High > 0:
		return "critical"
	case r.Counts.High > 0:
		return "high"
	case r.Counts.Medium >= 2:
		return "medium"
	default:
		return "low"
	}
}

// MarshalCompact emits the QuickResult as a single-line JSON payload suitable
// for piping into a shell hook. Hits are truncated to the first 10 entries to
// keep stdout under a kilobyte for the gate decision; the full set lives in
// the deep scan's audit.json.
func (r *QuickResult) MarshalCompact() ([]byte, error) {
	clone := *r
	if len(clone.Hits) > 10 {
		clone.Hits = clone.Hits[:10]
	}
	return json.Marshal(clone)
}

// SummaryLine returns a single human-readable one-liner the hook can show as
// permissionDecisionReason when blocking or asking.
func (r *QuickResult) SummaryLine() string {
	parts := []string{}
	if r.Counts.Critical > 0 {
		parts = append(parts, fmt.Sprintf("%d critical", r.Counts.Critical))
	}
	if r.Counts.High > 0 {
		parts = append(parts, fmt.Sprintf("%d high", r.Counts.High))
	}
	if r.Counts.Medium > 0 {
		parts = append(parts, fmt.Sprintf("%d medium", r.Counts.Medium))
	}
	if r.Counts.Secrets > 0 {
		parts = append(parts, fmt.Sprintf("%d secret hit(s)", r.Counts.Secrets))
	}
	if len(parts) == 0 {
		return "no pre-pass hits"
	}
	return strings.Join(parts, ", ")
}
