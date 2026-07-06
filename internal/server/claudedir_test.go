package server

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResolveClaudeDirPicksMarkedDir confirms the probe picks the candidate
// that actually has a plugins/ subdir over an empty one.
func TestResolveClaudeDirPicksMarkedDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses POSIX-style candidates; Windows path is a separate test")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	claude := filepath.Join(home, ".claude")
	require.NoError(t, os.MkdirAll(filepath.Join(claude, "plugins"), 0o750))

	got := ResolveClaudeDir()
	assert.Equal(t, claude, got)
}

// TestResolveClaudeDirFallsBackToDefaultWhenEmpty confirms the probe returns
// the first candidate even when nothing's populated yet (so a brand-new
// user with no Claude install still has a sensible default to mkdir into).
func TestResolveClaudeDirFallsBackToDefaultWhenEmpty(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses POSIX-style fallback path")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	// No .claude dir created — looksLikeClaudeDir returns false for all candidates.
	got := ResolveClaudeDir()
	assert.Equal(t, filepath.Join(home, ".claude"), got)
}

// TestDefaultClaudeDirCandidatesIncludesWindowsAppdata documents the
// Windows-specific search order even when the test runs on macOS/Linux —
// the candidate list is computed from runtime.GOOS so on non-Windows it
// returns the POSIX list. We verify it's non-empty.
func TestDefaultClaudeDirCandidatesNonEmpty(t *testing.T) {
	got := defaultClaudeDirCandidates()
	require.NotEmpty(t, got, "every OS should have at least one candidate")
	for _, c := range got {
		assert.True(t, filepath.IsAbs(c), "candidate %q must be absolute", c)
	}
}
