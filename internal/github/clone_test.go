package github

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCloneRejectsInvalidRef(t *testing.T) {
	cases := []struct {
		owner, repo string
	}{
		{"", "repo"},
		{"owner", ""},
		{"owner;rm -rf /", "repo"},   // shell injection attempt
		{"owner/sub", "repo"},        // path separator in owner
		{"owner", "../escape"},       // traversal
		{"owner", "repo with space"}, // space disallowed
		{".dotfile", "repo"},         // leading dot
	}
	for _, c := range cases {
		t.Run(c.owner+"/"+c.repo, func(t *testing.T) {
			_, err := Clone(context.Background(), c.owner, c.repo, t.TempDir(), nil)
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrInvalidRef),
				"want ErrInvalidRef for %q/%q, got %v", c.owner, c.repo, err)
		})
	}
}

func TestCloneAcceptsCanonicalRefs(t *testing.T) {
	// Valid owner/repo should pass validation. We don't network-clone in
	// tests — we exercise the validator by giving it valid refs and
	// asserting the failure is NOT ErrInvalidRef.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH; skipping clone smoke test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := Clone(ctx, "assay-test-nonexistent-org", "assay-test-nonexistent-repo", t.TempDir(), nil)
	if err == nil {
		t.Skip("clone unexpectedly succeeded; network env probably allows arbitrary hosts")
	}
	require.False(t, errors.Is(err, ErrInvalidRef),
		"valid ref should pass validation; got %v", err)
}

func TestEnsureGitInstalled(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		err := EnsureGitInstalled()
		require.Error(t, err)
		return
	}
	require.NoError(t, EnsureGitInstalled())
}

func TestCloneReturnsCachedDirOnFreshHit(t *testing.T) {
	// Build a fake "already cloned" directory and assert Clone short-circuits
	// without re-invoking git. This regression-guards the cache fast-path
	// against accidental removal during refactors.
	cache := t.TempDir()
	owner, repo := "anthropics", "claude-code-plugins"
	destDir := filepath.Join(cache, expectedCacheKey(owner, repo))
	require.NoError(t, os.MkdirAll(filepath.Join(destDir, ".git", "refs", "heads"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(destDir, ".git", "HEAD"),
		[]byte("ref: refs/heads/main\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(destDir, ".git", "refs", "heads", "main"),
		[]byte("0123456789abcdef0123456789abcdef01234567\n"), 0o600))

	res, err := Clone(context.Background(), owner, repo, cache, nil)
	require.NoError(t, err)
	assert.Equal(t, destDir, res.LocalPath)
	assert.Equal(t, "0123456789abcdef0123456789abcdef01234567", res.CommitSHA)
}

// expectedCacheKey reproduces the cache-key derivation in clone.go to keep
// the test independent. If clone.go ever changes its scheme, this test
// breaks first and forces an explicit update.
func expectedCacheKey(owner, repo string) string {
	h := sha256.Sum256([]byte(owner + "/" + repo))
	return strings.ToLower(owner) + "-" + strings.ToLower(repo) + "-" + hex.EncodeToString(h[:4])
}
