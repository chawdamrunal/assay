package inventory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHashDirStable(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "a.txt"), []byte("hello"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "b.txt"), []byte("world"), 0o600))

	h1, err := HashDir(tmp)
	require.NoError(t, err)
	h2, err := HashDir(tmp)
	require.NoError(t, err)
	assert.Equal(t, h1, h2, "hash must be deterministic")
	assert.True(t, strings.HasPrefix(h1, "sha256:"), "hash must be sha256-prefixed")
}

func TestHashDirChangesOnContentChange(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "a.txt"), []byte("hello"), 0o600))

	h1, err := HashDir(tmp)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(tmp, "a.txt"), []byte("HELLO"), 0o600))
	h2, err := HashDir(tmp)
	require.NoError(t, err)
	assert.NotEqual(t, h1, h2)
}

func TestHashDirChangesOnFileAdded(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "a.txt"), []byte("hello"), 0o600))

	h1, err := HashDir(tmp)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(tmp, "b.txt"), []byte("world"), 0o600))
	h2, err := HashDir(tmp)
	require.NoError(t, err)
	assert.NotEqual(t, h1, h2)
}

func TestHashDirIgnoresHiddenFiles(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "a.txt"), []byte("hello"), 0o600))

	h1, err := HashDir(tmp)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(tmp, ".DS_Store"), []byte("junk"), 0o600))
	h2, err := HashDir(tmp)
	require.NoError(t, err)
	assert.Equal(t, h1, h2, "hidden files should not affect hash")
}
