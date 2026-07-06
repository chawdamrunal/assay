package inventory

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// HashDir computes a deterministic sha256 over the contents of dir.
// File names are sorted so the result is path-order independent.
// Hidden files (names starting with ".") are excluded so editor/OS noise
// (.DS_Store, .git/) does not invalidate the hash.
//
// Returned value is prefixed: "sha256:<hex>".
func HashDir(dir string) (string, error) {
	type entry struct {
		rel  string
		size int64
	}
	var entries []entry

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		// Skip hidden entries and anything below them.
		base := filepath.Base(path)
		if strings.HasPrefix(base, ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		entries = append(entries, entry{rel: rel, size: info.Size()})
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("walk %s: %w", dir, err)
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })

	h := sha256.New()
	for _, e := range entries {
		if _, err := fmt.Fprintf(h, "%s\x00%d\x00", e.rel, e.size); err != nil {
			return "", fmt.Errorf("hash header %s: %w", e.rel, err)
		}
		f, err := os.Open(filepath.Join(dir, e.rel)) // #nosec G304 -- path joined from validated WalkDir entries under dir
		if err != nil {
			return "", fmt.Errorf("open %s: %w", e.rel, err)
		}
		if _, err := io.Copy(h, f); err != nil {
			_ = f.Close()
			return "", fmt.Errorf("read %s: %w", e.rel, err)
		}
		_ = f.Close()
	}

	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}
