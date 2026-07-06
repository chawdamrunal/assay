package api

import (
	"testing"

	"github.com/chawdamrunal/assay/internal/store"
)

// TestValidateConfig guards the PUT /api/config validation: a negative budget
// or out-of-range concurrency is rejected, while budget 0 (documented "no cap"
// for the subscription/MCP model) and a sane concurrency are accepted.
func TestValidateConfig(t *testing.T) {
	var c store.Config

	c.Scan.BudgetUSD = -1
	if err := validateConfig(c); err == nil {
		t.Error("negative budget must be rejected")
	}

	c.Scan.BudgetUSD = 0 // no cap — valid for subscription mode
	c.Scan.SubagentConcurrency = 0
	if err := validateConfig(c); err != nil {
		t.Errorf("zero budget + zero concurrency must be allowed, got %v", err)
	}

	c.Scan.SubagentConcurrency = 1000
	if err := validateConfig(c); err == nil {
		t.Error("excessive concurrency must be rejected")
	}

	c.Scan.SubagentConcurrency = 4
	c.Scan.BudgetUSD = 5
	if err := validateConfig(c); err != nil {
		t.Errorf("sane config must pass, got %v", err)
	}
}
