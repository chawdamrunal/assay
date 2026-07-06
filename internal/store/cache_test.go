package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A representative valid sha256 key (64 lowercase hex chars).
const testHashKey = "sha256:abc1234567890abc1234567890abc1234567890abc1234567890abc123456789"

func TestCacheMissAndHit(t *testing.T) {
	tmp := t.TempDir()
	c := NewCache(tmp)

	_, hit, err := c.Get(testHashKey)
	require.NoError(t, err)
	assert.False(t, hit)

	payload := []byte(`{"verdict":"safe"}`)
	require.NoError(t, c.Put(testHashKey, payload))

	got, hit, err := c.Get(testHashKey)
	require.NoError(t, err)
	assert.True(t, hit)
	assert.Equal(t, payload, got)
}

func TestCacheRejectsBadKey(t *testing.T) {
	tmp := t.TempDir()
	c := NewCache(tmp)

	err := c.Put("not-a-hash", []byte("x"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hash key")
}

func TestCacheRejectsShortKey(t *testing.T) {
	tmp := t.TempDir()
	c := NewCache(tmp)

	err := c.Put("sha256:abc123", []byte("x"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "64-hex-chars")
}

func TestCacheRejectsNonHexKey(t *testing.T) {
	tmp := t.TempDir()
	c := NewCache(tmp)

	// 64 chars but contains uppercase + 'g' (not hex)
	bad := "sha256:ABCDEFGHIJ234567890abc1234567890abc1234567890abc1234567890abc123"
	err := c.Put(bad, []byte("x"))
	require.Error(t, err)
}
