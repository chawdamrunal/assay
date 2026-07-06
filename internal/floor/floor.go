// Package floor applies Assay's deterministic finding "floor" — the findings
// that do not depend on an LLM and so must be identical across every scan mode
// (MCP, legacy/in-process, CLI). Today that floor is:
//
//   - SCA: transitive dependency CVEs via OSV.dev (internal/sca)
//   - poison: tool-poisoning / prompt-injection in tool descriptions
//     and skill/command markdown (internal/poison)
//
// Keeping this in one package fixes a real trust gap: before extraction, only
// the MCP finalize path (internal/mcp/verdict_assemble.go) applied the floor,
// so users on `--scan-mode legacy` / the CLI silently received a weaker audit.
//
// Import direction: floor imports sca, poison, and verdict. None of those
// import floor, and neither does scanner, so there is no cycle (scanner cannot
// import sca/poison directly because both import verdict, which imports
// scanner).
package floor

import (
	"context"
	"time"

	"github.com/chawdamrunal/assay/internal/poison"
	"github.com/chawdamrunal/assay/internal/sca"
	"github.com/chawdamrunal/assay/internal/verdict"
)

// scaTimeout bounds the OSV.dev round-trip so a slow network can't stall a scan.
const scaTimeout = 30 * time.Second

// Apply returns `in` plus the deterministic-floor findings for target. The SCA
// (OSV) lookup is skipped when offline (it needs the network); poison runs
// regardless (it is purely local). Both are best-effort: a floor analyzer that
// errors is skipped rather than failing the whole scan, mirroring the prior
// inline behavior in the MCP finalize path.
func Apply(ctx context.Context, target string, offline bool, in []verdict.Finding) []verdict.Finding {
	out := in
	if !offline {
		scaCtx, cancel := context.WithTimeout(ctx, scaTimeout)
		if r, err := sca.Analyze(scaCtx, target, false, nil); err == nil && r != nil {
			out = append(out, r.Findings...)
		}
		cancel()
	}
	if r, err := poison.Scan(target); err == nil && r != nil {
		out = append(out, r.Findings...)
	}
	return out
}
