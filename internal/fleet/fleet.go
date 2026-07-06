// Package fleet orchestrates scans across multiple targets (typically every
// installed Claude Code plugin) and aggregates the per-target verdicts into a
// single report a reviewer can read in one place.
//
// Architecturally fleet sits *above* the per-scan machinery: it does not
// duplicate scan execution, it composes existing StartScanFunc closures from
// the api package with a semaphore-bounded worker pool and a fan-in event
// merge. The on-disk layout for an individual scan stays under
// ~/.assay/scans/<target>/<scan_id>/; fleet adds a sibling tree at
// ~/.assay/fleet/<fleet_id>/ holding the membership manifest and (when all
// members complete) the aggregate report.
package fleet

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	assaymcp "github.com/chawdamrunal/assay/internal/mcp"
	"github.com/chawdamrunal/assay/internal/verdict"
)

// ErrFleetNotFound is returned by LoadMeta and Snapshot when no fleet with
// the requested ID exists on disk. The API handler uses this sentinel to
// translate a missing fleet into HTTP 404 instead of 500.
var ErrFleetNotFound = errors.New("fleet not found")

// Status tracks the lifecycle of a fleet scan.
type Status string

// Fleet-scan lifecycle states.
const (
	StatusRunning  Status = "running"
	StatusComplete Status = "complete"
	StatusFailed   Status = "failed"
)

// Member is one (target, scan_id) pair inside a fleet.
type Member struct {
	Target string `json:"target"`
	ScanID string `json:"scan_id"`
}

// Meta is the on-disk shape of <fleet_dir>/meta.json. Written when the fleet
// is allocated, updated on each member's completion.
type Meta struct {
	FleetID   string   `json:"fleet_id"`
	StartedAt string   `json:"started_at"`
	Status    Status   `json:"status"`
	Members   []Member `json:"members"`
	// Excludes records the plugin names the user asked to skip — useful when
	// reading the meta back to understand why a plugin isn't in the report.
	Excludes []string `json:"excludes,omitempty"`
}

// MemberReport is one row in a FleetReport — the bare facts the dashboard
// needs without loading the full audit.json.
type MemberReport struct {
	Target      string `json:"target"`
	ScanID      string `json:"scan_id"`
	Status      string `json:"status"` // "complete" | "failed" | "pending"
	Verdict     string `json:"verdict,omitempty"`
	Findings    int    `json:"findings,omitempty"`
	Critical    int    `json:"critical,omitempty"`
	High        int    `json:"high,omitempty"`
	Medium      int    `json:"medium,omitempty"`
	ErrorReason string `json:"error_reason,omitempty"`
}

// Report is the aggregate snapshot of a fleet — written to report.json once
// all members have terminated (success or failure) and returned by GET
// /api/fleet/:id at any time.
type Report struct {
	FleetID    string         `json:"fleet_id"`
	StartedAt  string         `json:"started_at"`
	FinishedAt string         `json:"finished_at,omitempty"`
	Status     Status         `json:"status"`
	Members    []MemberReport `json:"members"`
	// Aggregate counters across surviving (validated) findings of all
	// complete members. Failed/pending members do not contribute.
	Verdict struct {
		Safe    int `json:"safe"`
		Caution int `json:"caution"`
		Unsafe  int `json:"unsafe"`
	} `json:"verdict_counts"`
	Severity struct {
		Critical int `json:"critical"`
		High     int `json:"high"`
		Medium   int `json:"medium"`
		Low      int `json:"low"`
		Info     int `json:"info"`
	} `json:"severity_counts"`
}

// Store is a thin wrapper around the on-disk fleet directory tree. Mirrors
// the role of internal/mcp.ScanState for individual scans.
type Store struct {
	root string // typically ~/.assay/fleet
	mu   sync.Mutex
}

// NewStore returns a Store rooted at fleetDir (callers usually pass
// filepath.Join(paths.DataDir, "fleet")).
func NewStore(fleetDir string) *Store {
	return &Store{root: fleetDir}
}

// Allocate creates the fleet directory and persists initial meta.json.
func (s *Store) Allocate(fleetID string, members []Member, excludes []string) (string, error) {
	if fleetID == "" {
		return "", errors.New("fleet_id required")
	}
	if !validID(fleetID) {
		return "", fmt.Errorf("invalid fleet_id %q", fleetID)
	}
	dir := filepath.Join(s.root, fleetID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("mkdir fleet dir: %w", err)
	}
	meta := Meta{
		FleetID:   fleetID,
		StartedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Status:    StatusRunning,
		Members:   members,
		Excludes:  excludes,
	}
	data, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), data, 0o600); err != nil {
		return "", fmt.Errorf("write meta: %w", err)
	}
	return dir, nil
}

// LoadMeta reads <fleet_dir>/meta.json. Returns ErrFleetNotFound when no
// directory exists for the given ID (wrapped via %w so errors.Is callers
// continue to work either way).
func (s *Store) LoadMeta(fleetID string) (*Meta, error) {
	if !validID(fleetID) {
		return nil, fmt.Errorf("invalid fleet_id %q", fleetID)
	}
	data, err := os.ReadFile(filepath.Join(s.root, fleetID, "meta.json")) // #nosec G304 -- fleetID validated
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrFleetNotFound, fleetID)
		}
		return nil, err
	}
	var m Meta
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse fleet meta: %w", err)
	}
	return &m, nil
}

// SetStatus updates the meta.json status field. Best-effort: returns the
// error so callers can log but is otherwise safe to ignore (the disk write
// is not load-bearing — Snapshot recomputes status from member state).
func (s *Store) SetStatus(fleetID string, status Status) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.LoadMeta(fleetID)
	if err != nil {
		return err
	}
	m.Status = status
	data, _ := json.MarshalIndent(m, "", "  ")
	return os.WriteFile(filepath.Join(s.root, fleetID, "meta.json"), data, 0o600)
}

// Snapshot reads every member's audit.json + error.json off disk and produces
// the live aggregate report. Safe to call at any time during execution —
// returns partial data when members are still running.
func (s *Store) Snapshot(fleetID, scansDir string) (*Report, error) {
	meta, err := s.LoadMeta(fleetID)
	if err != nil {
		return nil, err
	}
	rep := &Report{
		FleetID:   meta.FleetID,
		StartedAt: meta.StartedAt,
		Status:    meta.Status,
		Members:   make([]MemberReport, 0, len(meta.Members)),
	}
	allTerminal := true
	for _, m := range meta.Members {
		mr := MemberReport{Target: m.Target, ScanID: m.ScanID, Status: "pending"}
		scanDir, err := assaymcp.FindScanDir(scansDir, m.ScanID)
		if err != nil {
			allTerminal = false // can't find scan dir yet
			rep.Members = append(rep.Members, mr)
			continue
		}
		// audit.json present → complete; error.json present → failed.
		if v, err := assaymcp.LoadVerdictFromDir(scanDir); err == nil {
			mr.Status = "complete"
			mr.Verdict = v.Verdict
			mr.Findings = len(v.Findings)
			for _, f := range v.Findings {
				switch f.Severity {
				case "critical":
					mr.Critical++
					rep.Severity.Critical++
				case "high":
					mr.High++
					rep.Severity.High++
				case "medium":
					mr.Medium++
					rep.Severity.Medium++
				case "low":
					rep.Severity.Low++
				case "info":
					rep.Severity.Info++
				}
			}
			switch v.Verdict {
			case "safe":
				rep.Verdict.Safe++
			case "caution":
				rep.Verdict.Caution++
			case "unsafe":
				rep.Verdict.Unsafe++
			}
		} else if errMsg, err := readErrorJSON(scanDir); err == nil {
			mr.Status = "failed"
			mr.ErrorReason = errMsg
		} else if memberAbandoned(scanDir) {
			// A pending member whose scan dir has had no activity in over an
			// hour is a dead subprocess that crashed before writing audit.json
			// or error.json. Mark it failed so the fleet can reach a terminal
			// state instead of reporting "running" forever (the single-scan
			// list endpoint applies the same heuristic).
			mr.Status = "failed"
			mr.ErrorReason = "abandoned: no audit.json or error.json after 1h of inactivity"
		} else {
			allTerminal = false
		}
		rep.Members = append(rep.Members, mr)
	}
	if allTerminal && rep.Status == StatusRunning {
		rep.Status = StatusComplete
		rep.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	return rep, nil
}

// WriteReport persists the snapshot to <fleet_dir>/report.json. Called once
// the fleet finishes so the dashboard can re-load without recomputing.
func (s *Store) WriteReport(fleetID string, rep *Report) error {
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.root, fleetID, "report.json"), data, 0o600)
}

// AppendEvent merges one per-member progress event into the fleet's
// events.jsonl. The envelope carries the scan_id so SSE consumers can
// route updates to the right per-plugin card in the UI.
func (s *Store) AppendEvent(fleetID, scanID string, ev assaymcp.ProgressEvent) error {
	if !validID(fleetID) {
		return fmt.Errorf("invalid fleet_id %q", fleetID)
	}
	entry := struct {
		ScanID  string `json:"scan_id"`
		Stage   string `json:"stage"`
		StatusF string `json:"status"`
		Message string `json:"message,omitempty"`
		At      string `json:"at"`
	}{ScanID: scanID, Stage: ev.Stage, StatusF: ev.Status, Message: ev.Message, At: ev.At}
	if entry.At == "" {
		entry.At = time.Now().UTC().Format(time.RFC3339Nano)
	}
	line, _ := json.Marshal(entry)
	line = append(line, '\n')
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.OpenFile(filepath.Join(s.root, fleetID, "events.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) // #nosec G304 -- fleetID validated
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.Write(line)
	return err
}

// List returns every fleet under root, most-recent-first. Used by GET
// /api/fleet to back the "past fleets" list page.
func (s *Store) List() ([]Meta, error) {
	entries, err := os.ReadDir(s.root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]Meta, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		m, err := s.LoadMeta(e.Name())
		if err != nil {
			continue // skip corrupt entries silently
		}
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt > out[j].StartedAt })
	return out, nil
}

// fleetAbandonedAge mirrors the single-scan abandonedAge: a pending member
// untouched this long is treated as dead. The spawn path enforces a 15-minute
// wall-clock deadline, so 1h leaves ample margin for a healthy long scan.
const fleetAbandonedAge = 1 * time.Hour

// memberAbandoned reports whether a still-pending member's scan dir has gone
// quiet long enough to be considered dead. It keys off the most recent of
// events.jsonl (appended during a live scan) and the dir itself, so an active
// long scan is never mistaken for abandoned.
func memberAbandoned(scanDir string) bool {
	ref := filepath.Join(scanDir, "events.jsonl")
	info, err := os.Stat(ref)
	if err != nil {
		info, err = os.Stat(scanDir)
		if err != nil {
			return false
		}
	}
	return time.Since(info.ModTime()) > fleetAbandonedAge
}

// LoadReport reads <fleet_dir>/report.json, written by the runner once the
// fleet finished. handleFleetGet prefers this over Snapshot so a completed
// fleet returns its persisted finished_at + aggregate instead of a recomputed
// snapshot whose finished_at would be empty. Returns ErrFleetNotFound when the
// report has not been written yet (fleet still running).
func (s *Store) LoadReport(fleetID string) (*Report, error) {
	if !validID(fleetID) {
		return nil, fmt.Errorf("invalid fleet_id %q", fleetID)
	}
	data, err := os.ReadFile(filepath.Join(s.root, fleetID, "report.json")) // #nosec G304 -- fleetID validated
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrFleetNotFound, fleetID)
		}
		return nil, err
	}
	var rep Report
	if err := json.Unmarshal(data, &rep); err != nil {
		return nil, fmt.Errorf("parse fleet report: %w", err)
	}
	return &rep, nil
}

// ValidID reports whether id is safe to use as a single path segment (no
// traversal, bounded charset). Exported so the HTTP layer can reject malformed
// fleet IDs before echoing them in error responses.
func ValidID(id string) bool { return validID(id) }

func readErrorJSON(scanDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(scanDir, "error.json")) // #nosec G304 -- caller-bounded
	if err != nil {
		return "", err
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return "", err
	}
	return body.Error, nil
}

func validID(id string) bool {
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
	for i := 0; i+1 < len(id); i++ {
		if id[i] == '.' && id[i+1] == '.' {
			return false
		}
	}
	return true
}

// scanner.Event compatibility shim: the fleet store accepts the verdict
// package's Severity strings, but we never directly import scanner here to
// keep the dependency graph one-directional.
var _ = (*verdict.Verdict)(nil)
