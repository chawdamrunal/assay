package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestEmbeddedHookMatchesCanonical asserts that the embedded copy of the
// pre-install hook (cmd/assay/hooks/assay-pre-install.sh) is byte-identical
// to the canonical source (plugin/hooks/assay-pre-install.sh).
//
// If this test fails, the embedded copy has drifted. Fix it by running:
//
//	cp plugin/hooks/assay-pre-install.sh cmd/assay/hooks/assay-pre-install.sh
//
// Go's //go:embed cannot reference paths outside the package directory, so the
// cmd/assay copy must be kept in sync with the canonical plugin copy by hand
// (or via CI). This test is the drift guard.
func TestEmbeddedHookMatchesCanonical(t *testing.T) {
	// Locate repository root relative to this test file's source location.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed — cannot locate repository root")
	}
	// thisFile is .../cmd/assay/hook_embed_test.go
	// repoRoot is ../.. from here
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")

	embeddedPath := filepath.Join(filepath.Dir(thisFile), "hooks", "assay-pre-install.sh")
	canonicalPath := filepath.Join(repoRoot, "plugin", "hooks", "assay-pre-install.sh")

	embedded, err := os.ReadFile(embeddedPath)
	if err != nil {
		t.Fatalf("reading embedded copy at %s: %v", embeddedPath, err)
	}
	canonical, err := os.ReadFile(canonicalPath)
	if err != nil {
		t.Fatalf("reading canonical copy at %s: %v", canonicalPath, err)
	}

	if string(embedded) != string(canonical) {
		t.Fatalf(
			"embedded hook has drifted from canonical source.\n"+
				"  embedded:  %s\n"+
				"  canonical: %s\n\n"+
				"Re-sync with:\n"+
				"  cp plugin/hooks/assay-pre-install.sh cmd/assay/hooks/assay-pre-install.sh",
			embeddedPath,
			canonicalPath,
		)
	}
}
