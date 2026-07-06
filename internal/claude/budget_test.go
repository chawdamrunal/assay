package claude

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBudgetTracksUsage(t *testing.T) {
	b := NewBudget(0.10) // 10 cents

	b.Charge(Usage{InputTokens: 1000, OutputTokens: 500}, "claude-sonnet-4-6")
	assert.Positive(t, b.SpentUSD())
	assert.Less(t, b.SpentUSD(), 0.10)
	assert.False(t, b.Exceeded())
}

func TestBudgetExceededAfterEnoughSpend(t *testing.T) {
	b := NewBudget(0.001) // ~1/10 cent

	b.Charge(Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000}, "claude-sonnet-4-6")
	assert.True(t, b.Exceeded())
}

func TestBudgetZeroCapMeansUnlimited(t *testing.T) {
	b := NewBudget(0)
	b.Charge(Usage{InputTokens: 1_000_000_000, OutputTokens: 1_000_000_000}, "claude-sonnet-4-6")
	assert.False(t, b.Exceeded(), "zero cap means unbounded")
}

func TestBudgetWrapperClientStopsAtExceeded(t *testing.T) {
	fc := NewFakeClient()
	fc.Enqueue(Response{Text: "ok", Stop: "end_turn", Usage: Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000}})
	fc.Enqueue(Response{Text: "should not see", Stop: "end_turn"})

	b := NewBudget(0.001)
	wrapped := NewBudgetClient(fc, b)

	_, err := wrapped.Complete(context.Background(), Request{Model: "claude-sonnet-4-6"})
	require.NoError(t, err)

	_, err = wrapped.Complete(context.Background(), Request{Model: "claude-sonnet-4-6"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrBudgetExceeded))
}

func TestBudgetWrapperPropagatesInnerError(t *testing.T) {
	fc := NewFakeClient()
	// no Enqueue, so first Complete returns "no enqueued response" error

	b := NewBudget(10.0)
	wrapped := NewBudgetClient(fc, b)
	_, err := wrapped.Complete(context.Background(), Request{Model: "claude-sonnet-4-6"})
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrBudgetExceeded), "inner error should not be ErrBudgetExceeded")
}

func TestBudgetUnknownModelUsesSonnetPrice(t *testing.T) {
	b := NewBudget(0)
	b.Charge(Usage{InputTokens: 1_000_000}, "claude-some-future-model-9000")
	assert.Equal(t, 3.0, b.SpentUSD()) // sonnet input price = $3 / Mtok
}
