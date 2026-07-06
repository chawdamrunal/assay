package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chawdamrunal/assay/internal/verdict"
)

// FindPriorScan returns the most recent completed scan of targetName excluding
// excludeScanID (typically the scan currently in progress). Returns
// (scanID, scanDir, nil) on success or ("", "", os.ErrNotExist) when no prior
// scan exists.
//
// More lenient than store.History.List: that helper only matches timestamp
// IDs; this one walks all dirs and sorts by mtime, so UUID-IDed scans
// (produced by the HTTP API path) are also discovered.
//
// A scan is "completed" iff its dir contains audit.json. In-progress and
// failed scans (audit.json absent, error.json possibly present) are skipped.
func FindPriorScan(scansDir, targetName, excludeScanID string) (string, string, error) {
	if targetName == "" {
		return "", "", errors.New("target name required")
	}
	targetDir := filepath.Join(scansDir, targetName)
	entries, err := os.ReadDir(targetDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", "", os.ErrNotExist
		}
		return "", "", fmt.Errorf("read target dir: %w", err)
	}

	type candidate struct {
		id      string
		dir     string
		modTime int64
	}
	var cands []candidate
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if e.Name() == excludeScanID {
			continue
		}
		dir := filepath.Join(targetDir, e.Name())
		auditPath := filepath.Join(dir, "audit.json")
		info, err := os.Stat(auditPath)
		if err != nil {
			continue // scan didn't complete; skip
		}
		cands = append(cands, candidate{
			id:      e.Name(),
			dir:     dir,
			modTime: info.ModTime().UnixNano(),
		})
	}
	if len(cands) == 0 {
		return "", "", os.ErrNotExist
	}
	sort.Slice(cands, func(i, j int) bool {
		return cands[i].modTime > cands[j].modTime
	})
	return cands[0].id, cands[0].dir, nil
}

// LoadVerdictFromDir reads and parses audit.json from a scan dir. Used by
// the auto-diff path to fetch prior findings, and by /api/scans/diff to load
// both sides of the comparison.
func LoadVerdictFromDir(scanDir string) (*verdict.Verdict, error) {
	auditPath := filepath.Join(scanDir, "audit.json")
	data, err := os.ReadFile(auditPath) // #nosec G304 -- caller-supplied, bounded by HTTP layer's scansDir
	if err != nil {
		return nil, err
	}
	var v verdict.Verdict
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("parse audit.json at %s: %w", scanDir, err)
	}
	return &v, nil
}

// FindScanDir resolves a scan_id to its on-disk dir by walking scansDir/*
// (target dirs). Mirrors the lookup logic in api.handleGetScan but as a
// package-level helper so other call sites (e.g. /api/scans/diff) can reuse it.
func FindScanDir(scansDir, scanID string) (string, error) {
	if scanID == "" {
		return "", errors.New("scan_id required")
	}
	// Cheap path-escape guard; the api layer also calls safeScanID but this
	// helper is exposed so we belt-and-suspender.
	if strings.Contains(scanID, "/") || strings.Contains(scanID, "..") {
		return "", fmt.Errorf("invalid scan_id %q", scanID)
	}
	entries, err := os.ReadDir(scansDir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		candidate := filepath.Join(scansDir, e.Name(), scanID)
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate, nil
		}
	}
	return "", os.ErrNotExist
}
