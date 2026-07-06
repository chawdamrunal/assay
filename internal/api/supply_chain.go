package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/chawdamrunal/assay/internal/verdict"
)

// SupplyChainSummary aggregates SCA + poison findings across every
// completed scan on this host. The dashboard tile reads from this
// endpoint so users see "X vulnerable dependencies, Y poisoned tools"
// at a glance without opening individual reports.
type SupplyChainSummary struct {
	// Counts of severities across SCA-class findings only.
	DependencyCritical int `json:"dependency_critical"`
	DependencyHigh     int `json:"dependency_high"`
	DependencyMedium   int `json:"dependency_medium"`
	// Poison findings broken out separately so the dashboard can render
	// "X plugins ship a poisoned tool description" as its own signal.
	PoisonFindings int `json:"poison_findings"`
	// AffectedPlugins is the count of distinct plugin targets with at
	// least one SCA or poison finding.
	AffectedPlugins int `json:"affected_plugins"`
	// TotalScans is the denominator — useful for "12 of 30 plugins
	// have known-vulnerable transitives" framing.
	TotalScans int `json:"total_scans"`
}

// NewSupplyChainHandler implements GET /api/supply-chain/summary. It
// walks the on-disk scan store, reads each audit.json, and aggregates
// findings whose category is "dependency" (SCA) or "tool_poisoning"
// (poison detector).
func NewSupplyChainHandler(scansDir string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			WriteJSONError(w, http.StatusMethodNotAllowed, "GET only")
			return
		}
		summary := computeSupplyChainSummary(scansDir)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(summary)
	})
}

// computeSupplyChainSummary aggregates per-scan audit.json files into a
// single fleet-level summary. Best-effort: a corrupt or unreadable
// audit.json is skipped without failing the whole request.
//
// Concurrency: file reads are parallelised across plugins because a
// fleet with 50+ plugins would otherwise add ~1s of sequential I/O to
// every dashboard refresh.
func computeSupplyChainSummary(scansDir string) SupplyChainSummary {
	var (
		mu       sync.Mutex
		summary  SupplyChainSummary
		affected = map[string]bool{}
	)

	targets, err := os.ReadDir(scansDir)
	if err != nil {
		return summary
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, t := range targets {
		if !t.IsDir() {
			continue
		}
		t := t
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			// Walk all scans of this target; the most recent scan wins
			// (older scans of the same target might have stale CVE data).
			latest := mostRecentAuditJSON(filepath.Join(scansDir, t.Name()))
			if latest == "" {
				return
			}
			data, err := os.ReadFile(latest) // #nosec G304 -- scan-dir-bound
			if err != nil {
				return
			}
			var v verdict.Verdict
			if err := json.Unmarshal(data, &v); err != nil {
				return
			}
			var hasIssue bool
			for _, f := range v.Findings {
				switch f.Category {
				case "dependency":
					mu.Lock()
					switch f.Severity {
					case "critical":
						summary.DependencyCritical++
					case "high":
						summary.DependencyHigh++
					case "medium":
						summary.DependencyMedium++
					}
					mu.Unlock()
					hasIssue = true
				case "tool_poisoning":
					mu.Lock()
					summary.PoisonFindings++
					mu.Unlock()
					hasIssue = true
				}
			}
			mu.Lock()
			summary.TotalScans++
			if hasIssue {
				affected[t.Name()] = true
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	summary.AffectedPlugins = len(affected)
	return summary
}

// mostRecentAuditJSON returns the path of the audit.json under the most
// recently modified scan-id subdir of targetDir. Returns "" when no
// audit.json is present (only failed or pending scans).
func mostRecentAuditJSON(targetDir string) string {
	scans, err := os.ReadDir(targetDir)
	if err != nil {
		return ""
	}
	var bestPath string
	var bestMtime int64
	for _, s := range scans {
		if !s.IsDir() {
			continue
		}
		audit := filepath.Join(targetDir, s.Name(), "audit.json")
		info, err := os.Stat(audit)
		if err != nil {
			continue
		}
		if info.ModTime().Unix() > bestMtime {
			bestMtime = info.ModTime().Unix()
			bestPath = audit
		}
	}
	return bestPath
}
