package main

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"

	"github.com/chawdamrunal/assay/internal/auth"
)

func TestAuthStatusNoCredentials(t *testing.T) {
	keyring.MockInit()
	t.Setenv(auth.EnvVar, "")
	configKeyringService = "assay-test"

	// Swap claudeCodeReader via the auth package so neither real keychain
	// nor an installed Claude Code interferes.
	auth.SetClaudeCodeReaderForTesting(func() (string, error) { return "", nil })
	t.Cleanup(auth.ResetClaudeCodeReaderForTesting)

	root := newRootCmd()
	root.SetArgs([]string{"auth", "status"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	require.NoError(t, root.Execute())

	output := out.String()
	assert.Contains(t, output, "no credentials available")
	assert.Contains(t, output, "env")
	assert.Contains(t, output, "assay-key")
	assert.Contains(t, output, "claude-code")
}

func TestAuthStatusEnvWins(t *testing.T) {
	keyring.MockInit()
	t.Setenv(auth.EnvVar, "sk-ant-fake-env-value-12345") // #nosec G101 -- test fixture, not a real credential
	configKeyringService = "assay-test"
	auth.SetClaudeCodeReaderForTesting(func() (string, error) { return "", nil })
	t.Cleanup(auth.ResetClaudeCodeReaderForTesting)

	root := newRootCmd()
	root.SetArgs([]string{"auth", "status"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	require.NoError(t, root.Execute())

	output := out.String()
	assert.Contains(t, output, "Active: env")
	assert.Contains(t, output, "api-key")
}

func TestAuthStatusJSONOutput(t *testing.T) {
	keyring.MockInit()
	t.Setenv(auth.EnvVar, "")
	configKeyringService = "assay-test"

	exp := time.Now().Add(24 * time.Hour).UnixMilli()
	body, _ := json.Marshal(map[string]any{
		"claudeAiOauth": map[string]any{
			"accessToken":      "sk-ant-oat01-fake", // #nosec G101 -- test fixture
			"expiresAt":        exp,
			"subscriptionType": "max",
		},
	})
	auth.SetClaudeCodeReaderForTesting(func() (string, error) { return string(body), nil })
	t.Cleanup(auth.ResetClaudeCodeReaderForTesting)

	root := newRootCmd()
	root.SetArgs([]string{"auth", "status", "--json"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	require.NoError(t, root.Execute())

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &parsed))
	assert.Equal(t, "claude-code", parsed["active"])
	methods, ok := parsed["methods"].([]any)
	require.True(t, ok)
	assert.Len(t, methods, 3)
}
