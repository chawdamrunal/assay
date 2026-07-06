package prepass

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScanSecretsFindsAWSKey(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "config.py"),
		[]byte("AWS_ACCESS_KEY_ID = \"AKIAIOSFODNN7EXAMPLE\"\nfoo = 1\n"), 0o600))

	hits, err := ScanSecrets(dir, Options{})
	require.NoError(t, err)
	require.NotEmpty(t, hits)

	var found bool
	for _, h := range hits {
		if h.Category == "secret" && h.Line == 1 {
			found = true
			assert.Contains(t, h.Message, "AWS")
			assert.Contains(t, h.Snippet, "AKIAIOSFODNN7EXAMPLE")
		}
	}
	assert.True(t, found, "expected AWS key hit at line 1")
}

func TestScanSecretsFindsAnthropicKey(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "client.js"),
		[]byte("const key = \"sk-ant-api03-abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789abc\";\n"),
		0o600))

	hits, err := ScanSecrets(dir, Options{})
	require.NoError(t, err)

	var found bool
	for _, h := range hits {
		if h.Category == "secret" {
			found = true
			assert.Contains(t, h.Message, "Anthropic")
		}
	}
	assert.True(t, found, "expected Anthropic key hit")
}

func TestScanSecretsIgnoresLowEntropy(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "main.go"),
		[]byte("package main\nfunc main() { println(\"hello\") }\n"), 0o600))

	hits, err := ScanSecrets(dir, Options{})
	require.NoError(t, err)
	assert.Empty(t, hits, "plain code should not trigger secret hits")
}

func TestScanSecretsSkipsLargeFiles(t *testing.T) {
	dir := t.TempDir()
	big := make([]byte, 2<<20)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "big.bin"), big, 0o600))

	hits, err := ScanSecrets(dir, Options{MaxFileSize: 1 << 20})
	require.NoError(t, err)
	for _, h := range hits {
		assert.NotEqual(t, "big.bin", filepath.Base(h.File), "large file should be skipped")
	}
}

func TestScanSecretsSkipsHiddenDirs(t *testing.T) {
	dir := t.TempDir()
	hidden := filepath.Join(dir, ".git")
	require.NoError(t, os.MkdirAll(hidden, 0o750))
	require.NoError(t, os.WriteFile(
		filepath.Join(hidden, "config"),
		[]byte("AKIAIOSFODNN7EXAMPLE\n"), 0o600))

	hits, err := ScanSecrets(dir, Options{})
	require.NoError(t, err)
	assert.Empty(t, hits, ".git/ should be skipped")
}
