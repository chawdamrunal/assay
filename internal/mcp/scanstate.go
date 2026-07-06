package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ScanState owns the on-disk layout of an in-progress scan. The MCP server
// uses it to allocate scan directories, append progress events, append
// findings, and finalize a verdict. The HTTP server (assay serve) tails the
// same files for SSE, so this is the single source of truth across both
// surfaces. Concurrency-safe: any number of MCP clients can write to the
// same scan_id, last-writer-wins on meta.json.
type ScanState struct {
	root string // typically ~/.assay/scans
	// offline, when true, tells the deterministic floor (assembleVerdict) to
	// skip the OSV.dev network lookup. It is set from the MCP server's
	// --offline flag so an air-gapped scan honors offline end-to-end (the
	// methodology prompt already steers the LLM away from network tools; this
	// covers the server-side SCA floor that runs regardless of the prompt).
	offline bool
	mu      sync.Mutex
}

// NewScanState returns a ScanState rooted at scansDir (online).
func NewScanState(scansDir string) *ScanState {
	return &ScanState{root: scansDir}
}

// NewScanStateWithOffline returns a ScanState that propagates the offline flag
// into the deterministic floor at finalize time.
func NewScanStateWithOffline(scansDir string, offline bool) *ScanState {
	return &ScanState{root: scansDir, offline: offline}
}

// DeriveTargetName is the exported form of deriveTargetName so callers outside
// this package (fleet runner, serve diff/resume) bucket scans by the same name
// the MCP allocator uses — avoiding the filepath.Base(version-dir) collision
// for marketplace plugins installed under .../cache/<m>/<name>/<version>/.
func DeriveTargetName(target string) string {
	return deriveTargetName(target)
}

// Meta is the per-scan metadata persisted at <scan_dir>/meta.json.
type Meta struct {
	ScanID    string `json:"scan_id"`
	Target    string `json:"target"`
	StartedAt string `json:"started_at"`
	Status    string `json:"status"` // running / complete / failed
}

// ProgressEvent is one line in events.jsonl. Matches scanner.Event shape so
// the SSE bridge can re-emit without translation.
type ProgressEvent struct {
	Stage   string `json:"stage"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	At      string `json:"at"`
}

// Allocate creates a scan directory and writes meta.json. The directory path
// is scansDir/<targetName>/<scan_id> where targetName is derived from the
// target path heuristically — see deriveTargetName. Callers that know the
// canonical plugin name (e.g. scan-all reading inventory) should use
// AllocateAs to pass it explicitly.
func (s *ScanState) Allocate(scanID, target string) (string, error) {
	return s.AllocateAs(scanID, target, "")
}

// AllocateAs is Allocate with an explicit targetName override. When
// targetName is empty, falls back to deriveTargetName(target) — which is the
// inventory-aware fix for the install-path layout
// ~/.claude/plugins/cache/<marketplace>/<name>/<version>/, where
// filepath.Base would (wrongly) return the version subdir.
func (s *ScanState) AllocateAs(scanID, target, targetName string) (string, error) {
	if scanID == "" {
		return "", fmt.Errorf("scan_id is required")
	}
	if !validScanID(scanID) {
		return "", fmt.Errorf("invalid scan_id %q", scanID)
	}
	if target == "" {
		return "", fmt.Errorf("target is required")
	}
	if targetName == "" {
		targetName = deriveTargetName(target)
	}
	scanDir := filepath.Join(s.root, targetName, scanID)
	if err := os.MkdirAll(scanDir, 0o750); err != nil {
		return "", fmt.Errorf("mkdir scan dir: %w", err)
	}
	meta := Meta{
		ScanID:    scanID,
		Target:    target,
		StartedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Status:    "running",
	}
	data, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.WriteFile(filepath.Join(scanDir, "meta.json"), data, 0o600); err != nil {
		return "", fmt.Errorf("write meta.json: %w", err)
	}
	return scanDir, nil
}

// deriveTargetName extracts a usable bucket name from a target path. For the
// Claude Code plugin install layout
//
//	~/.claude/plugins/cache/<marketplace>/<name>/<version>
//
// filepath.Base returns the version subdir (e.g. "1.0.0" or a git SHA),
// causing scans of different plugins with the same version to collide. We
// detect that layout by looking for "/cache/<m>/<name>/<version>" and
// returning <name> instead.
//
// For all other paths (an arbitrary directory, a /testdata/ fixture, a
// project dir), filepath.Base is correct.
func deriveTargetName(target string) string {
	clean := filepath.Clean(target)
	parts := strings.Split(clean, string(filepath.Separator))
	// Look for the cache layout: ".../plugins/cache/<marketplace>/<name>/<version>"
	for i := 0; i < len(parts)-3; i++ {
		if parts[i] == "plugins" && parts[i+1] == "cache" {
			// parts[i+3] is the plugin name; parts[i+4] is the version
			if i+3 < len(parts) {
				return parts[i+3]
			}
		}
	}
	return filepath.Base(clean)
}

// AppendEvent atomically appends one JSON line to events.jsonl in the scan
// dir for scanID. The MCP server calls this from assay_emit_progress; the
// HTTP SSE bridge tails the same file.
func (s *ScanState) AppendEvent(scanID string, ev ProgressEvent) error {
	scanDir, err := s.findScanDir(scanID)
	if err != nil {
		return err
	}
	if ev.At == "" {
		ev.At = time.Now().UTC().Format(time.RFC3339Nano)
	}
	line, _ := json.Marshal(ev)
	line = append(line, '\n')
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.OpenFile(filepath.Join(scanDir, "events.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) // #nosec G304 -- bounded under scansDir
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.Write(line)
	return err
}

// AppendFinding atomically appends one JSON-encoded finding to findings.jsonl
// in the scan dir. finalize_scan reads this back to produce audit.json.
func (s *ScanState) AppendFinding(scanID string, finding map[string]any) error {
	scanDir, err := s.findScanDir(scanID)
	if err != nil {
		return err
	}
	line, _ := json.Marshal(finding)
	line = append(line, '\n')
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.OpenFile(filepath.Join(scanDir, "findings.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) // #nosec G304 -- bounded under scansDir
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.Write(line)
	return err
}

// LoadFindings reads back every finding appended for scanID, in order. Used
// by Finalize to assemble the final verdict body.
func (s *ScanState) LoadFindings(scanID string) ([]map[string]any, error) {
	scanDir, err := s.findScanDir(scanID)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(scanDir, "findings.jsonl")) // #nosec G304 -- bounded
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []map[string]any
	for _, line := range splitJSONLines(data) {
		var rec map[string]any
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		out = append(out, rec)
	}
	return out, nil
}

// ScanDir returns the absolute path of the scan directory for scanID, or an
// error if it cannot be located.
func (s *ScanState) ScanDir(scanID string) (string, error) {
	return s.findScanDir(scanID)
}

// WriteAudit persists audit.json and audit.md for a finalized scan. Updates
// meta.json status to "complete".
func (s *ScanState) WriteAudit(scanID string, auditJSON []byte, auditMarkdown string) (string, error) {
	scanDir, err := s.findScanDir(scanID)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(scanDir, "audit.json"), auditJSON, 0o600); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(scanDir, "audit.md"), []byte(auditMarkdown), 0o600); err != nil {
		return "", err
	}
	// Best-effort status update.
	meta, _ := os.ReadFile(filepath.Join(scanDir, "meta.json")) // #nosec G304 -- bounded
	var m Meta
	if err := json.Unmarshal(meta, &m); err == nil {
		m.Status = "complete"
		updated, _ := json.MarshalIndent(m, "", "  ")
		_ = os.WriteFile(filepath.Join(scanDir, "meta.json"), updated, 0o600)
	}
	return scanDir, nil
}

// findScanDir locates a scan by ID under the root.
// Layout: <root>/<target_basename>/<scan_id>/, so we walk one level.
func (s *ScanState) findScanDir(scanID string) (string, error) {
	if !validScanID(scanID) {
		return "", fmt.Errorf("invalid scan_id %q", scanID)
	}
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return "", fmt.Errorf("read scans root: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		candidate := filepath.Join(s.root, e.Name(), scanID)
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("scan %s not found", scanID)
}

// validScanID accepts UUIDs and CLI timestamp IDs (digits, letters, hyphen,
// underscore, dot). Mirrors api.safeScanID — the two must stay aligned.
func validScanID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.':
		default:
			return false
		}
	}
	// Reject ".." and any sequence containing it.
	for i := 0; i+1 < len(id); i++ {
		if id[i] == '.' && id[i+1] == '.' {
			return false
		}
	}
	return true
}

func splitJSONLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			if i > start {
				lines = append(lines, data[start:i])
			}
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}
