package prepass

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScanPatternsFindsEval(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "danger.js"),
		[]byte("function r(input) { return eval(input); }\n"), 0o600))

	hits, err := ScanPatterns(dir, Options{})
	require.NoError(t, err)
	require.NotEmpty(t, hits)

	var found bool
	for _, h := range hits {
		if h.Metadata["rule"] == "js-eval" {
			found = true
			assert.Contains(t, h.Snippet, "eval(")
			assert.Equal(t, "info", h.Severity, "patterns produce info-severity evidence")
		}
	}
	assert.True(t, found)
}

func TestScanPatternsFindsChildProcess(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "exec.js"),
		[]byte("const cp = require('child_process');\ncp.exec(cmd);\n"), 0o600))

	hits, err := ScanPatterns(dir, Options{})
	require.NoError(t, err)

	rules := map[string]bool{}
	for _, h := range hits {
		rules[h.Metadata["rule"]] = true
	}
	assert.True(t, rules["js-child-process"], "expected child_process hit")
}

func TestScanPatternsFindsSshOrAwsRead(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "creds.go"),
		[]byte(`package main
import "os"
func main() { _, _ = os.ReadFile("~/.aws/credentials") }`), 0o600))

	hits, err := ScanPatterns(dir, Options{})
	require.NoError(t, err)

	rules := map[string]bool{}
	for _, h := range hits {
		rules[h.Metadata["rule"]] = true
	}
	assert.True(t, rules["sensitive-path-read"], "expected sensitive-path-read hit for ~/.aws/credentials")
}

func TestScanPatternsCleanCode(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "good.go"),
		[]byte("package main\nfunc main() { println(\"hello\") }\n"), 0o600))

	hits, err := ScanPatterns(dir, Options{})
	require.NoError(t, err)
	assert.Empty(t, hits)
}
