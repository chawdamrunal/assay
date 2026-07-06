package api

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnsureAllowedRejectsPathOutsideRoots(t *testing.T) {
	root := t.TempDir()
	_, err := EnsureAllowed("/etc/passwd", []string{root})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrPathNotAllowed), "want ErrPathNotAllowed, got %v", err)
}

func TestEnsureAllowedAcceptsPathInsideRoot(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "plugin", "main.js")
	got, err := EnsureAllowed(target, []string{root})
	require.NoError(t, err)
	assert.Equal(t, filepath.Clean(target), got)
}

func TestEnsureAllowedRejectsDotDotEscape(t *testing.T) {
	root := t.TempDir()
	// Attempt to escape via `../../../etc/passwd` even when prefixed with root.
	target := filepath.Join(root, "..", "..", "etc", "passwd")
	_, err := EnsureAllowed(target, []string{root})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrPathNotAllowed))
}

func TestEnsureAllowedRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir() // a second tempdir NOT in allowed roots
	link := filepath.Join(root, "escape-link")
	require.NoError(t, os.Symlink(outside, link))

	// The symlink lexically lives under root, but resolves outside it.
	target := filepath.Join(link, "secret.txt")
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("x"), 0o600))

	_, err := EnsureAllowed(target, []string{root})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrPathNotAllowed),
		"symlink-escape must be rejected; got %v", err)
}

func TestEnsureAllowedRejectsEmptyInput(t *testing.T) {
	_, err := EnsureAllowed("   ", []string{t.TempDir()})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrPathNotAllowed))
}

func TestEnsureAllowedFailsClosedWhenNoRoots(t *testing.T) {
	_, err := EnsureAllowed("/tmp/anything", nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrPathNotAllowed))
}

func TestEnsureAllowedAcceptsNonExistentChild(t *testing.T) {
	// A scan target may not exist yet (e.g. user typo) — that's the scanner's
	// problem to surface, not the guard's. As long as the path lexically lives
	// under an allowed root, the guard permits it.
	root := t.TempDir()
	target := filepath.Join(root, "does", "not", "exist", "yet")
	got, err := EnsureAllowed(target, []string{root})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(got, root))
}

func TestEnsureAllowedAcceptsMultipleRoots(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	target := filepath.Join(b, "thing")
	_, err := EnsureAllowed(target, []string{a, b})
	require.NoError(t, err)
}

// TestDefaultAllowedRootsExcludesTempDir regression-guards the security fix
// that removed os.TempDir() from the default trusted roots. On macOS the temp
// tree is world-writable; trusting it let any local process plant a scan
// target at a predictable path and have assay serve read+exfiltrate it.
func TestDefaultAllowedRootsExcludesTempDir(t *testing.T) {
	roots := DefaultAllowedRoots(t.TempDir())
	for _, r := range roots {
		assert.NotEqual(t, os.TempDir(), r, "os.TempDir() must not be a default allowed root")
	}

	// A path under the OS temp dir must be rejected against the default roots.
	tmpTarget := filepath.Join(os.TempDir(), "assay-evil-plugin", "main.js")
	_, err := EnsureAllowed(tmpTarget, roots)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrPathNotAllowed),
		"a target under the OS temp dir must not be allowed by default; got %v", err)
}

func TestEnsureAllowedRejectsSiblingPrefixCollision(t *testing.T) {
	// /tmp/assay is allowed; /tmp/assay-foo should NOT be accepted as a
	// prefix match (string prefix would say yes; isUnder correctly says no).
	parent := t.TempDir()
	allowed := filepath.Join(parent, "assay")
	sibling := filepath.Join(parent, "assay-foo")
	require.NoError(t, os.MkdirAll(allowed, 0o750))
	require.NoError(t, os.MkdirAll(sibling, 0o750))

	_, err := EnsureAllowed(filepath.Join(sibling, "file"), []string{allowed})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrPathNotAllowed))
}
