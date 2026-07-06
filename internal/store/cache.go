package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Cache stores raw scan-result blobs keyed by target hash (sha256:hex).
// Two scans of the same target with the same hash short-circuit to the
// cached verdict — saves API spend on unchanged plugins.
type Cache struct {
	dir string
}

// NewCache returns a Cache rooted at dir.
func NewCache(dir string) *Cache {
	return &Cache{dir: dir}
}

// Get returns the cached bytes for hashKey. hit=false means cache miss.
func (c *Cache) Get(hashKey string) ([]byte, bool, error) {
	path, err := c.pathFor(hashKey)
	if err != nil {
		return nil, false, err
	}
	data, err := os.ReadFile(path) // #nosec G304 -- path derived from validated sha256 hashKey under c.dir
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("cache read: %w", err)
	}
	return data, true, nil
}

// Put stores payload under hashKey.
func (c *Cache) Put(hashKey string, payload []byte) error {
	path, err := c.pathFor(hashKey)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("cache mkdir: %w", err)
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		return fmt.Errorf("cache write: %w", err)
	}
	return nil
}

func (c *Cache) pathFor(hashKey string) (string, error) {
	const prefix = "sha256:"
	const hexLen = 64
	if !strings.HasPrefix(hashKey, prefix) {
		return "", fmt.Errorf("invalid hash key %q (expected sha256:<64-hex-chars>)", hashKey)
	}
	hex := strings.TrimPrefix(hashKey, prefix)
	if len(hex) != hexLen {
		return "", fmt.Errorf("invalid hash key %q (expected sha256:<64-hex-chars>, got %d hex chars)", hashKey, len(hex))
	}
	if !isLowerHex(hex) {
		return "", fmt.Errorf("invalid hash key %q (non-hex characters)", hashKey)
	}
	// Shard by first 2 hex chars to avoid huge flat directories.
	return filepath.Join(c.dir, hex[:2], hex+".json"), nil
}

func isLowerHex(s string) bool {
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
