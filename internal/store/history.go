package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// History manages per-target scan directories under a root (typically Paths.ScansDir).
//
// Layout:
//
//	<root>/<target-name>/<scan-id>/audit.md
//	<root>/<target-name>/<scan-id>/audit.json
//	<root>/<target-name>/<scan-id>/investigation.log
//	<root>/<target-name>/<scan-id>/evidence/...
type History struct {
	root string
}

// Record is a single scan directory's metadata for listing.
type Record struct {
	Target string
	ID     string
	Dir    string
	Time   time.Time
}

// NewHistory returns a History rooted at root.
func NewHistory(root string) *History {
	return &History{root: root}
}

// Allocate creates a fresh scan directory for target and returns its path and ID.
// IDs are sortable timestamps (YYYYMMDDTHHMMSSZnnn). If two calls collide within
// the same millisecond, a "-NNN" suffix disambiguates.
func (h *History) Allocate(target string) (string, string, error) {
	if target == "" {
		return "", "", errors.New("target name cannot be empty")
	}
	if strings.Contains(target, "..") || strings.ContainsRune(target, filepath.Separator) || strings.ContainsRune(target, '/') {
		return "", "", fmt.Errorf("invalid target name %q", target)
	}
	parent := filepath.Join(h.root, target)
	if err := os.MkdirAll(parent, 0o750); err != nil {
		return "", "", fmt.Errorf("mkdir scan parent: %w", err)
	}
	base := time.Now().UTC().Format("20060102T150405.000Z")
	for i := 0; i < 1000; i++ {
		id := base
		if i > 0 {
			id = fmt.Sprintf("%s-%03d", base, i)
		}
		dir := filepath.Join(parent, id)
		err := os.Mkdir(dir, 0o750)
		if err == nil {
			return dir, id, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", "", fmt.Errorf("mkdir scan dir: %w", err)
		}
	}
	return "", "", errors.New("history.Allocate: exhausted 1000 collision suffixes (this should never happen)")
}

// AllocateAt creates a scan directory under target with the caller-provided id.
// Unlike Allocate, the id is supplied by the caller (e.g., a UUID from the API
// layer that needs to be known before the directory is created). Returns the
// directory path. Errors if target or id is empty, or if the directory already exists.
func (h *History) AllocateAt(target, id string) (string, error) {
	if target == "" {
		return "", errors.New("target name cannot be empty")
	}
	if strings.Contains(target, "..") || strings.ContainsRune(target, filepath.Separator) || strings.ContainsRune(target, '/') {
		return "", fmt.Errorf("invalid target name %q", target)
	}
	if id == "" {
		return "", errors.New("id cannot be empty")
	}
	parent := filepath.Join(h.root, target)
	if err := os.MkdirAll(parent, 0o750); err != nil {
		return "", fmt.Errorf("mkdir scan parent: %w", err)
	}
	dir := filepath.Join(parent, id)
	if err := os.Mkdir(dir, 0o750); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return "", fmt.Errorf("scan dir already exists: %s", dir)
		}
		return "", fmt.Errorf("mkdir scan dir: %w", err)
	}
	return dir, nil
}

// List returns the scan history for target, most recent first.
func (h *History) List(target string) ([]Record, error) {
	dir := filepath.Join(h.root, target)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read history dir: %w", err)
	}

	records := make([]Record, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// Strip optional "-NNN" collision suffix before parsing the timestamp.
		name := e.Name()
		stamp := name
		if idx := strings.LastIndex(name, "-"); idx > 0 {
			stamp = name[:idx]
		}
		t, err := time.Parse("20060102T150405.000Z", stamp)
		if err != nil {
			continue // ignore unparsable directory names
		}
		records = append(records, Record{
			Target: target,
			ID:     name,
			Dir:    filepath.Join(dir, name),
			Time:   t,
		})
	}

	sort.Slice(records, func(i, j int) bool {
		if records[i].Time.Equal(records[j].Time) {
			// Same-millisecond entries: sort by ID descending so higher suffix comes first.
			return records[i].ID > records[j].ID
		}
		return records[i].Time.After(records[j].Time)
	})
	return records, nil
}
