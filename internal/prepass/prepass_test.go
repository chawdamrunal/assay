package prepass

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunAggregatesEverything(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "leak.py"),
		[]byte("KEY = \"sk-ant-api03-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\"\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "danger.js"),
		[]byte("eval(req.body.code)\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"),
		[]byte(`{"name":"x","version":"1.0.0"}`), 0o600))

	r, err := Run(dir, Options{Offline: true})
	require.NoError(t, err)
	assert.Equal(t, dir, r.Target)
	assert.False(t, r.RanAt.IsZero())

	cats := map[string]int{}
	for _, h := range r.Hits {
		cats[h.Category]++
	}
	assert.Positive(t, cats["secret"])
	assert.Positive(t, cats["pattern"])
	assert.Contains(t, r.Manifests, filepath.Join(dir, "package.json"))
}

func TestRunOfflineSkipsOSV(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n"), 0o600))

	r, err := Run(dir, Options{Offline: true})
	require.NoError(t, err)
	for _, h := range r.Hits {
		assert.NotEqual(t, "cve", h.Category, "OSV should not run in offline mode")
	}
}

func TestRunSkipsNodeModulesAndVendor(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "node_modules", "foo"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "node_modules", "foo", "package.json"),
		[]byte(`{"name":"foo","version":"1.0.0"}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"),
		[]byte(`{"name":"x","version":"1.0.0"}`), 0o600))

	r, err := Run(dir, Options{Offline: true})
	require.NoError(t, err)
	require.Len(t, r.Manifests, 1, "node_modules manifests should be excluded")
	assert.Equal(t, filepath.Join(dir, "package.json"), r.Manifests[0])
}
