package store

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHistoryAllocateAndList(t *testing.T) {
	tmp := t.TempDir()
	h := NewHistory(tmp)

	dir1, id1, err := h.Allocate("rainbow-formatter")
	require.NoError(t, err)
	assert.NotEmpty(t, id1)
	assert.Equal(t, filepath.Join(tmp, "rainbow-formatter", id1), dir1)
	assert.True(t, dirExists(dir1))

	dir2, id2, err := h.Allocate("rainbow-formatter")
	require.NoError(t, err)
	assert.NotEqual(t, id1, id2)

	records, err := h.List("rainbow-formatter")
	require.NoError(t, err)
	assert.Len(t, records, 2)
	assert.Equal(t, dir2, records[0].Dir, "most recent first")
	assert.Equal(t, dir1, records[1].Dir)
}

func TestHistoryListEmpty(t *testing.T) {
	tmp := t.TempDir()
	h := NewHistory(tmp)

	records, err := h.List("nonexistent")
	require.NoError(t, err)
	assert.Empty(t, records)
}

func TestHistoryAllocateAtUsesProvidedID(t *testing.T) {
	tmp := t.TempDir()
	h := NewHistory(tmp)

	dir, err := h.AllocateAt("rainbow-formatter", "abc-123")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tmp, "rainbow-formatter", "abc-123"), dir)
	assert.True(t, dirExists(dir))
}

func TestHistoryAllocateAtRejectsEmpty(t *testing.T) {
	tmp := t.TempDir()
	h := NewHistory(tmp)

	_, err := h.AllocateAt("", "id")
	assert.Error(t, err)
	_, err = h.AllocateAt("target", "")
	assert.Error(t, err)
}

func TestHistoryAllocateAtRejectsDuplicate(t *testing.T) {
	tmp := t.TempDir()
	h := NewHistory(tmp)

	_, err := h.AllocateAt("target", "fixed-id")
	require.NoError(t, err)
	_, err = h.AllocateAt("target", "fixed-id")
	assert.Error(t, err)
}

func TestAllocateRejectsTraversal(t *testing.T) {
	tmp := t.TempDir()
	h := NewHistory(tmp)

	traversalInputs := []string{"../evil", "a/b", ".."}
	for _, bad := range traversalInputs {
		_, _, err := h.Allocate(bad)
		assert.Error(t, err, "Allocate(%q) should be rejected", bad)

		_, err = h.AllocateAt(bad, "some-id")
		assert.Error(t, err, "AllocateAt(%q, ...) should be rejected", bad)
	}

	// A normal name must succeed.
	_, _, err := h.Allocate("my-plugin")
	assert.NoError(t, err, "Allocate(\"my-plugin\") should succeed")
}

func TestHistoryAllocateDisambiguatesCollisions(t *testing.T) {
	tmp := t.TempDir()
	h := NewHistory(tmp)

	// Rapid-fire 5 allocations; some will collide within the same millisecond.
	seen := map[string]struct{}{}
	for i := 0; i < 5; i++ {
		dir, id, err := h.Allocate("burst")
		require.NoError(t, err)
		assert.True(t, dirExists(dir))
		_, dupe := seen[id]
		assert.False(t, dupe, "Allocate must produce unique IDs even under rapid calls; saw %q twice", id)
		seen[id] = struct{}{}
	}
}
