package auth

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"
)

func TestFromEnvSet(t *testing.T) {
	t.Setenv(EnvVar, "sk-ant-test-value")
	c := FromEnv()
	require.NotNil(t, c)
	assert.Equal(t, KindAPIKey, c.Kind)
	assert.Equal(t, "sk-ant-test-value", c.APIKey)
	assert.Equal(t, MethodEnv, c.Source)
}

func TestFromEnvUnset(t *testing.T) {
	t.Setenv(EnvVar, "")
	assert.Nil(t, FromEnv())
}

func TestFromClaudeCodeValidBlob(t *testing.T) {
	exp := time.Now().Add(24 * time.Hour).UnixMilli()
	blob := map[string]any{
		"claudeAiOauth": map[string]any{ // #nosec G101 -- test fixture, not a real credential
			"accessToken":      "sk-ant-oat01-fake-token-for-test",
			"refreshToken":     "sk-ant-ort01-fake-refresh",
			"expiresAt":        exp,
			"scopes":           []string{"user:inference"},
			"subscriptionType": "max",
		},
	}
	body, _ := json.Marshal(blob)
	claudeCodeReader = func() (string, error) { return string(body), nil }
	t.Cleanup(func() { claudeCodeReader = readClaudeCodeFromOSKeychain })

	c := FromClaudeCode()
	require.NotNil(t, c)
	assert.Equal(t, KindBearer, c.Kind)
	assert.Equal(t, "sk-ant-oat01-fake-token-for-test", c.BearerToken)
	assert.Equal(t, MethodClaudeCode, c.Source)
	assert.Equal(t, "max", c.Subscription)
	assert.False(t, c.Expired())
}

func TestFromClaudeCodeExpired(t *testing.T) {
	exp := time.Now().Add(-1 * time.Hour).UnixMilli()
	blob := map[string]any{
		"claudeAiOauth": map[string]any{ // #nosec G101 -- test fixture, not a real credential
			"accessToken": "sk-ant-oat01-expired",
			"expiresAt":   exp,
		},
	}
	body, _ := json.Marshal(blob)
	claudeCodeReader = func() (string, error) { return string(body), nil }
	t.Cleanup(func() { claudeCodeReader = readClaudeCodeFromOSKeychain })

	assert.Nil(t, FromClaudeCode(), "expired token should be treated as unavailable")
}

func TestFromClaudeCodeMissing(t *testing.T) {
	claudeCodeReader = func() (string, error) { return "", nil }
	t.Cleanup(func() { claudeCodeReader = readClaudeCodeFromOSKeychain })

	assert.Nil(t, FromClaudeCode())
}

func TestResolveOrder(t *testing.T) {
	keyring.MockInit()
	t.Setenv(EnvVar, "")
	// Assay keychain is empty; Claude Code reader returns valid token.
	exp := time.Now().Add(24 * time.Hour).UnixMilli()
	blob := map[string]any{
		"claudeAiOauth": map[string]any{ // #nosec G101 -- test fixture, not a real credential
			"accessToken":      "sk-ant-oat01-from-claude-code",
			"expiresAt":        exp,
			"subscriptionType": "pro",
		},
	}
	body, _ := json.Marshal(blob)
	claudeCodeReader = func() (string, error) { return string(body), nil }
	t.Cleanup(func() { claudeCodeReader = readClaudeCodeFromOSKeychain })

	c, err := Resolve("assay-test")
	require.NoError(t, err)
	require.NotNil(t, c)
	assert.Equal(t, MethodClaudeCode, c.Source)
	assert.Equal(t, KindBearer, c.Kind)
}

func TestResolveEnvWins(t *testing.T) {
	keyring.MockInit()
	t.Setenv(EnvVar, "sk-ant-env-key")
	exp := time.Now().Add(24 * time.Hour).UnixMilli()
	blob := map[string]any{
		"claudeAiOauth": map[string]any{ // #nosec G101 -- test fixture, not a real credential
			"accessToken": "sk-ant-oat01-claude-code",
			"expiresAt":   exp,
		},
	}
	body, _ := json.Marshal(blob)
	claudeCodeReader = func() (string, error) { return string(body), nil }
	t.Cleanup(func() { claudeCodeReader = readClaudeCodeFromOSKeychain })

	c, err := Resolve("assay-test")
	require.NoError(t, err)
	assert.Equal(t, MethodEnv, c.Source, "env should win over claude code")
	assert.Equal(t, "sk-ant-env-key", c.APIKey)
}

func TestResolveNoCredentials(t *testing.T) {
	keyring.MockInit()
	t.Setenv(EnvVar, "")
	claudeCodeReader = func() (string, error) { return "", nil }
	t.Cleanup(func() { claudeCodeReader = readClaudeCodeFromOSKeychain })

	_, err := Resolve("assay-test")
	require.ErrorIs(t, err, ErrNoCredentials)
}

func TestResolveAllReturnsStatuses(t *testing.T) {
	keyring.MockInit()
	t.Setenv(EnvVar, "")
	claudeCodeReader = func() (string, error) { return "", nil }
	t.Cleanup(func() { claudeCodeReader = readClaudeCodeFromOSKeychain })

	_, statuses := ResolveAll("assay-test")
	require.Len(t, statuses, 3)
	methods := map[Method]bool{}
	for _, s := range statuses {
		methods[s.Method] = true
		assert.False(t, s.Available, "all methods should be unavailable in clean state")
	}
	assert.True(t, methods[MethodEnv])
	assert.True(t, methods[MethodAssayKey])
	assert.True(t, methods[MethodClaudeCode])
}
