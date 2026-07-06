//go:build smoke

package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chawdamrunal/assay/internal/claude"
)

// TestRealAPIScanRainbow runs a real Sonnet-driven scan against the safe
// `rainbow-formatter` corpus fixture using the actual Anthropic API.
//
// Requires ANTHROPIC_API_KEY in the environment. Skips otherwise.
// Expected cost: ~$0.10-$0.30.
//
// To run:
//
//	ANTHROPIC_API_KEY=sk-ant-... go test -tags smoke ./internal/scanner/ -run TestRealAPI -v
func TestRealAPIScanRainbow(t *testing.T) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set — skipping real-API smoke test")
	}

	repoRoot := findRepoRootSmoke(t)
	fixture := filepath.Join(repoRoot, "testdata", "corpus", "safe", "rainbow-formatter")

	client, err := claude.NewRealClient(apiKey, nil)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	budget := claude.NewBudget(2.00) // hard cap at $2
	wrapped := claude.NewBudgetClient(client, budget)

	events := make(chan Event, 128)
	go func() {
		for e := range events {
			t.Logf("[%s] %s %s", e.Stage, e.Status, e.Message)
		}
	}()

	v, err := Scan(ctx, Options{
		Target:              fixture,
		Model:               "claude-sonnet-4-6",
		SubagentConcurrency: 2,
		Offline:             true,
	}, wrapped, events)
	close(events)

	require.NoError(t, err)
	require.NotNil(t, v)
	t.Logf("Verdict: %s", v.Verdict)
	t.Logf("Findings: %d", len(v.Findings))
	t.Logf("Cost: $%.4f", budget.SpentUSD())

	assert.Equal(t, "safe", v.Verdict, "rainbow-formatter is a clean fixture; expected verdict=safe")
}

// findRepoRootSmoke walks up from the test's CWD to locate go.mod.
//
// A local copy lives here (rather than reusing corpus_test.go's findRepoRoot)
// because that file uses `//go:build integration`, which does not compose with
// our `//go:build smoke` tag.
func findRepoRootSmoke(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repo root not found from " + wd)
		}
		dir = parent
	}
}
