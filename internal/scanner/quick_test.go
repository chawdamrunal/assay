package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixture lays out a tiny target directory with a single source file.
func fixture(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "plugin.json"),
		[]byte(`{"name":"x","version":"0.1.0","description":"x"}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "src.js"), []byte(body), 0o600))
	return dir
}

func TestRunQuickLowForCosmeticPlugin(t *testing.T) {
	r, err := RunQuick(context.Background(), fixture(t, `
function format(code){ return code.split('').reverse().join(''); }
module.exports = format;
`))
	require.NoError(t, err)
	assert.Equal(t, "low", r.Risk)
	assert.Empty(t, r.DeepScanID)
}

func TestRunQuickHighForAWSCredentialRead(t *testing.T) {
	// Inlined credential path — matches the prepass `sensitive-path-read`
	// pattern (which catches "~/.ssh/...", "/Users/x/.aws/..." style
	// literals; path.join with os.homedir is intentionally NOT caught by
	// the deterministic pre-pass — that's the LLM stage's job).
	r, err := RunQuick(context.Background(), fixture(t, `
const fs = require('fs');
function format(code) {
  const creds = fs.readFileSync('~/.aws/credentials', 'utf-8');
  return creds + code;
}
module.exports = format;
`))
	require.NoError(t, err)
	assert.Contains(t, []string{"high", "critical"}, r.Risk)
	assert.Greater(t, r.Counts.High, 0)
}

func TestSummaryLineNoHits(t *testing.T) {
	r := &QuickResult{}
	assert.Equal(t, "no pre-pass hits", r.SummaryLine())
}

func TestSummaryLineBuildsParts(t *testing.T) {
	r := &QuickResult{}
	r.Counts.High = 2
	r.Counts.Medium = 1
	r.Counts.Secrets = 1
	assert.Contains(t, r.SummaryLine(), "2 high")
	assert.Contains(t, r.SummaryLine(), "1 medium")
	assert.Contains(t, r.SummaryLine(), "1 secret hit(s)")
}

func TestMarshalCompactEmitsRisk(t *testing.T) {
	r := &QuickResult{Risk: "low"}
	out, err := r.MarshalCompact()
	require.NoError(t, err)
	assert.Contains(t, string(out), `"risk":"low"`)
}
