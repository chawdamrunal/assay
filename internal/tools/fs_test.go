package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadFileBounded(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "hello.txt"), []byte("hello world\nsecond line\n"), 0o600))

	fs := NewFS(root)

	r, err := fs.ReadFile(context.Background(), Invocation{Input: map[string]any{"path": "hello.txt"}})
	require.NoError(t, err)
	assert.Contains(t, r.Text, "hello world")
}

// TestSymbolRefsClassifiesDefsAndRefs guards the taint-tracing tool: it must
// find the definition AND the reference of a symbol across files and put each
// in the right bucket.
func TestSymbolRefsClassifiesDefsAndRefs(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "creds.js"),
		[]byte("function readCreds() {\n  return fs.readFileSync('.aws/credentials');\n}\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.js"),
		[]byte("const data = readCreds();\nsend(data);\n"), 0o600))

	fs := NewFS(root)
	r, err := fs.SymbolRefs(context.Background(), Invocation{Input: map[string]any{"symbol": "readCreds"}})
	require.NoError(t, err)

	assert.Contains(t, r.Text, "Definitions of \"readCreds\"")
	assert.Contains(t, r.Text, "creds.js:1:", "the function definition must be found")
	assert.Contains(t, r.Text, "main.js:1:", "the call site must be found as a reference")
}

func TestSymbolRefsNoMatch(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "x.go"), []byte("package x\n"), 0o600))
	fs := NewFS(root)
	r, err := fs.SymbolRefs(context.Background(), Invocation{Input: map[string]any{"symbol": "nonexistentSymbol"}})
	require.NoError(t, err)
	assert.Contains(t, r.Text, "no references")
}

func TestSymbolRefsRequiresSymbol(t *testing.T) {
	fs := NewFS(t.TempDir())
	_, err := fs.SymbolRefs(context.Background(), Invocation{Input: map[string]any{"symbol": "  "}})
	require.Error(t, err)
}

func TestSymbolRefsWordBoundary(t *testing.T) {
	root := t.TempDir()
	// "token" must not match "tokenizer" / "subtoken".
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.go"),
		[]byte("var token = 1\nvar tokenizer = 2\nfoo(subtoken)\n"), 0o600))
	fs := NewFS(root)
	r, err := fs.SymbolRefs(context.Background(), Invocation{Input: map[string]any{"symbol": "token"}})
	require.NoError(t, err)
	assert.Contains(t, r.Text, "a.go:1:")
	assert.NotContains(t, r.Text, "a.go:2:", "tokenizer must not match the word 'token'")
	assert.NotContains(t, r.Text, "a.go:3:", "subtoken must not match the word 'token'")
}

// TestGrepSignalsTruncation guards that grep tells the caller when it hit the
// match cap, so a partial result isn't mistaken for "all matches".
func TestGrepSignalsTruncation(t *testing.T) {
	root := t.TempDir()
	content := ""
	for i := 0; i < 80; i++ {
		content += "needle here\n"
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "big.txt"), []byte(content), 0o600))

	fs := NewFS(root)
	r, err := fs.Grep(context.Background(), Invocation{Input: map[string]any{"pattern": "needle"}})
	require.NoError(t, err)
	assert.Contains(t, r.Text, "TRUNCATED", "grep must signal when results are capped")
}

// TestGrepSkipsNodeModules guards that bundled dependency trees don't consume
// the match budget (and thereby hide the plugin's own files).
func TestGrepSkipsNodeModules(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "node_modules", "dep"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, "node_modules", "dep", "x.js"), []byte("SECRET_NEEDLE\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "app.js"), []byte("clean code\n"), 0o600))

	fs := NewFS(root)
	r, err := fs.Grep(context.Background(), Invocation{Input: map[string]any{"pattern": "SECRET_NEEDLE"}})
	require.NoError(t, err)
	assert.Equal(t, "(no matches)", r.Text, "grep must skip node_modules")
}

// TestReadFileRejectsHugeFile regression-guards the DoS fix: ReadFile must not
// load a file larger than its ceiling fully into memory; it returns a guard
// message instead so a bundled multi-megabyte blob can't blow up the heap.
func TestReadFileRejectsHugeFile(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "blob.bin"), make([]byte, 11<<20), 0o600))

	fs := NewFS(root)
	r, err := fs.ReadFile(context.Background(), Invocation{Input: map[string]any{"path": "blob.bin"}})
	require.NoError(t, err)
	assert.Contains(t, r.Text, "too large")
}

func TestReadFileRejectsEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret"), []byte("nope"), 0o600))

	fs := NewFS(root)
	_, err := fs.ReadFile(context.Background(), Invocation{Input: map[string]any{"path": "../" + filepath.Base(outside) + "/secret"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "escapes root")
}

func TestReadFileLineRange(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "x.txt"),
		[]byte("a\nb\nc\nd\ne\nf\n"), 0o600))

	fs := NewFS(root)
	r, err := fs.ReadFile(context.Background(), Invocation{Input: map[string]any{
		"path":       "x.txt",
		"start_line": float64(2),
		"end_line":   float64(4),
	}})
	require.NoError(t, err)
	assert.Contains(t, r.Text, "b")
	assert.Contains(t, r.Text, "c")
	assert.Contains(t, r.Text, "d")
	assert.NotContains(t, r.Text, "a\n")
	assert.NotContains(t, r.Text, "e\n")
}

func TestListDir(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "sub"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "sub", "b.txt"), []byte("b"), 0o600))

	fs := NewFS(root)
	r, err := fs.ListDir(context.Background(), Invocation{Input: map[string]any{"path": "."}})
	require.NoError(t, err)
	assert.Contains(t, r.Text, "a.txt")
	assert.Contains(t, r.Text, "sub/")
}

func TestGrep(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "code.py"),
		[]byte("import os\nos.system(cmd)\nfoo = 1\n"), 0o600))

	fs := NewFS(root)
	r, err := fs.Grep(context.Background(), Invocation{Input: map[string]any{"pattern": "os\\.system"}})
	require.NoError(t, err)
	assert.Contains(t, r.Text, "code.py:2")
	assert.Contains(t, r.Text, "os.system(cmd)")
}

func TestGrepInvalidPattern(t *testing.T) {
	root := t.TempDir()
	fs := NewFS(root)
	_, err := fs.Grep(context.Background(), Invocation{Input: map[string]any{"pattern": "[unclosed"}})
	require.Error(t, err)
}

func TestDefsReturnsAllThree(t *testing.T) {
	fs := NewFS(t.TempDir())
	defs := fs.Defs()
	names := map[string]bool{}
	for _, d := range defs {
		names[d.Name] = true
	}
	assert.True(t, names["read_file"])
	assert.True(t, names["list_dir"])
	assert.True(t, names["grep"])
}
