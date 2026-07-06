package stats

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Stat returns line count and size for relPath relative to root.
// Rejects absolute paths and paths containing ".." segments.
func Stat(root, relPath string) (lines int, size int64, err error) {
	if filepath.IsAbs(relPath) {
		return 0, 0, errors.New("absolute paths not allowed")
	}
	if strings.Contains(relPath, "..") {
		return 0, 0, errors.New("path traversal not allowed")
	}
	full := filepath.Join(root, relPath)
	info, err := os.Stat(full)
	if err != nil {
		return 0, 0, err
	}
	size = info.Size()

	f, err := os.Open(full)
	if err != nil {
		return 0, size, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines++
	}
	return lines, size, scanner.Err()
}
