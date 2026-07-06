package store

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"
)

func TestKeyringRoundTrip(t *testing.T) {
	keyring.MockInit()

	kr := NewKeyring("assay-test")

	_, err := kr.GetAPIKey()
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAPIKeyNotSet), "expected ErrAPIKeyNotSet, got %v", err)

	require.NoError(t, kr.SetAPIKey("sk-ant-test-key-123"))

	got, err := kr.GetAPIKey()
	require.NoError(t, err)
	assert.Equal(t, "sk-ant-test-key-123", got)

	require.NoError(t, kr.DeleteAPIKey())

	_, err = kr.GetAPIKey()
	assert.True(t, errors.Is(err, ErrAPIKeyNotSet))
}

func TestKeyringRejectsEmpty(t *testing.T) {
	keyring.MockInit()
	kr := NewKeyring("assay-test")
	err := kr.SetAPIKey("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestKeyringPerProvider(t *testing.T) {
	keyring.MockInit()
	kr := NewKeyring("assay-test")

	require.NoError(t, kr.SetProviderKey("gemini-api", "g-key"))
	require.NoError(t, kr.SetProviderKey("openai-api", "o-key"))

	g, err := kr.GetProviderKey("gemini-api")
	require.NoError(t, err)
	assert.Equal(t, "g-key", g)
	assert.True(t, kr.HasProviderKey("gemini-api"))
	assert.False(t, kr.HasProviderKey("anthropic-api"))

	// The Anthropic shim and the "anthropic-api" provider key are the SAME
	// keychain entry (backward compatibility with `assay config set api-key`).
	require.NoError(t, kr.SetAPIKey("a-key"))
	a, err := kr.GetProviderKey("anthropic-api")
	require.NoError(t, err)
	assert.Equal(t, "a-key", a)

	// Entries stay isolated per provider.
	o, err := kr.GetProviderKey("openai-api")
	require.NoError(t, err)
	assert.Equal(t, "o-key", o)
}

func TestProviderKeyEntryBackwardCompat(t *testing.T) {
	// The anthropic entry label must remain "anthropic-api-key" so keys set by
	// older builds / `assay config set api-key` are still found.
	assert.Equal(t, "anthropic-api-key", providerKeyEntry("anthropic-api"))
	assert.Equal(t, "gemini-api-key", providerKeyEntry("gemini-api"))
	assert.Equal(t, "openai-api-key", providerKeyEntry("openai-api"))
}
