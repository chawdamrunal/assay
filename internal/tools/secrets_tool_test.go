package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSecretsToolScansSubPath(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "leak.py"),
		[]byte("KEY = \"AKIAIOSFODNN7EXAMPLE\"\n"), 0o600))

	tool := NewSecretsTool(root)
	r, err := tool.Scan(context.Background(), Invocation{Input: map[string]any{"path": "."}})
	require.NoError(t, err)
	assert.Contains(t, r.Text, "AKIAIOSFODNN7EXAMPLE")
	assert.Contains(t, r.Text, "leak.py")
}

func TestSecretsToolEscapeRejected(t *testing.T) {
	tool := NewSecretsTool(t.TempDir())
	_, err := tool.Scan(context.Background(), Invocation{Input: map[string]any{"path": "../escape"}})
	require.Error(t, err)
}

func TestSecretsToolEmpty(t *testing.T) {
	tool := NewSecretsTool(t.TempDir())
	r, err := tool.Scan(context.Background(), Invocation{Input: map[string]any{"path": "."}})
	require.NoError(t, err)
	assert.Contains(t, r.Text, "(no secrets found)")
}
