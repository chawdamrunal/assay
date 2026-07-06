package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func TestPaths(t *testing.T) {
	p, err := NewPaths()
	if err != nil {
		t.Fatalf("NewPaths failed: %v", err)
	}

	if !filepath.IsAbs(p.ConfigFile) {
		t.Errorf("ConfigFile should be absolute, got %q", p.ConfigFile)
	}
	if !strings.HasSuffix(p.ConfigFile, filepath.Join("assay", "config.toml")) {
		t.Errorf("ConfigFile should end with assay/config.toml, got %q", p.ConfigFile)
	}

	if !filepath.IsAbs(p.DataDir) {
		t.Errorf("DataDir should be absolute, got %q", p.DataDir)
	}
	if !strings.HasSuffix(p.DataDir, ".assay") {
		t.Errorf("DataDir should end with .assay, got %q", p.DataDir)
	}

	if p.ScansDir != filepath.Join(p.DataDir, "scans") {
		t.Errorf("ScansDir should be DataDir/scans, got %q", p.ScansDir)
	}
	if p.CacheDir != filepath.Join(p.DataDir, "cache") {
		t.Errorf("CacheDir should be DataDir/cache, got %q", p.CacheDir)
	}
}

func TestPathsEnsure(t *testing.T) {
	tmp := t.TempDir()
	p := &Paths{
		ConfigFile: filepath.Join(tmp, "cfg", "config.toml"),
		ConfigDir:  filepath.Join(tmp, "cfg"),
		DataDir:    filepath.Join(tmp, "data"),
		ScansDir:   filepath.Join(tmp, "data", "scans"),
		CacheDir:   filepath.Join(tmp, "data", "cache"),
	}

	if err := p.Ensure(); err != nil {
		t.Fatalf("Ensure failed: %v", err)
	}

	for _, dir := range []string{p.ConfigDir, p.DataDir, p.ScansDir, p.CacheDir} {
		if !dirExists(dir) {
			t.Errorf("expected %q to exist after Ensure", dir)
		}
	}
}
