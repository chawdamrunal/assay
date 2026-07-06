package floor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chawdamrunal/assay/internal/verdict"
)

// TestApplyEmptyTargetReturnsInputUnchanged confirms the floor is additive and
// safe on a target with nothing to find: an empty dir scanned offline (no SCA
// network call) and with no poisonable manifests yields exactly the input.
func TestApplyEmptyTargetReturnsInputUnchanged(t *testing.T) {
	in := []verdict.Finding{{ID: "F-1", Severity: "high", Title: "from the LLM"}}

	out := Apply(context.Background(), t.TempDir(), true /* offline */, in)

	require.Len(t, out, 1, "empty offline target adds no floor findings")
	assert.Equal(t, "F-1", out[0].ID)
}

// TestApplyPreservesInputOrdering confirms floor findings are appended after
// the provided (LLM) findings, never reordered.
func TestApplyPreservesInputOrdering(t *testing.T) {
	in := []verdict.Finding{
		{ID: "A", Severity: "low"},
		{ID: "B", Severity: "medium"},
	}
	out := Apply(context.Background(), t.TempDir(), true, in)
	require.GreaterOrEqual(t, len(out), 2)
	assert.Equal(t, "A", out[0].ID)
	assert.Equal(t, "B", out[1].ID)
}
