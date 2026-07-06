package claude

import (
	"context"
	"errors"
	"sync"
)

// ErrBudgetExceeded is returned by BudgetClient.Complete after the per-scan
// budget has been spent. The scanner orchestrator treats this as a graceful
// stop signal and records "budget exceeded" in open_questions.
var ErrBudgetExceeded = errors.New("budget exceeded")

// pricePerMillionTokens — approximate USD pricing per million tokens, as of
// the v0 design date (2026-05). These are not authoritative; the scanner
// reports actual cost in the verdict so users can sanity-check. Update via
// PRs as Anthropic changes pricing.
var pricePerMillionTokens = map[string]struct{ Input, Output, CacheRead float64 }{
	"claude-sonnet-4-6": {Input: 3.00, Output: 15.00, CacheRead: 0.30},
	"claude-opus-4-7":   {Input: 15.00, Output: 75.00, CacheRead: 1.50},
}

// Budget tracks USD spend against a configured cap.
type Budget struct {
	mu     sync.Mutex
	capUSD float64
	spent  float64
}

// NewBudget constructs a Budget with the given cap.
// capUSD <= 0 means unbounded (Exceeded always returns false).
func NewBudget(capUSD float64) *Budget {
	return &Budget{capUSD: capUSD}
}

// Charge records token usage against the budget.
func (b *Budget) Charge(u Usage, model string) {
	price, ok := pricePerMillionTokens[model]
	if !ok {
		price = pricePerMillionTokens["claude-sonnet-4-6"]
	}
	cost := float64(u.InputTokens)/1e6*price.Input +
		float64(u.OutputTokens)/1e6*price.Output +
		float64(u.CacheReadTokens)/1e6*price.CacheRead +
		float64(u.CacheCreationTokens)/1e6*price.Input
	b.mu.Lock()
	b.spent += cost
	b.mu.Unlock()
}

// SpentUSD returns the accumulated spend in USD.
func (b *Budget) SpentUSD() float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.spent
}

// CapUSD returns the configured cap.
func (b *Budget) CapUSD() float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.capUSD
}

// Exceeded reports whether spend has hit the cap. Caps <= 0 are unbounded.
func (b *Budget) Exceeded() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.capUSD > 0 && b.spent >= b.capUSD
}

// BudgetClient wraps a Client and charges every successful response against a Budget.
// Calls after Exceeded() returns true short-circuit with ErrBudgetExceeded without
// hitting the underlying client.
type BudgetClient struct {
	inner  Client
	budget *Budget
}

// Compile-time check that BudgetClient satisfies Client.
var _ Client = (*BudgetClient)(nil)

// NewBudgetClient wraps inner with budget tracking.
func NewBudgetClient(inner Client, b *Budget) *BudgetClient {
	return &BudgetClient{inner: inner, budget: b}
}

// Complete delegates to the inner client and charges usage.
// Returns ErrBudgetExceeded if the budget is already over the cap when called.
func (b *BudgetClient) Complete(ctx context.Context, req Request) (Response, error) {
	if b.budget.Exceeded() {
		return Response{}, ErrBudgetExceeded
	}
	resp, err := b.inner.Complete(ctx, req)
	if err != nil {
		return resp, err
	}
	b.budget.Charge(resp.Usage, req.Model)
	return resp, nil
}
