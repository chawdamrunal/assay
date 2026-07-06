package policy

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chawdamrunal/assay/internal/verdict"
)

func writePolicy(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, DefaultFileName)
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	return p
}

func TestLoadValid(t *testing.T) {
	p, err := Load(writePolicy(t, `{
		"suppress": [{"id": "SCA-CVE-2021-1234", "reason": "accepted risk, no upgrade path"}],
		"deny_categories": ["exfiltration"],
		"allowlist": ["my-internal-plugin"],
		"fail_on": "caution"
	}`))
	require.NoError(t, err)
	assert.Len(t, p.Suppress, 1)
	assert.Equal(t, []string{"exfiltration"}, p.DenyCategories)
	assert.Equal(t, "caution", p.FailOn)
}

func TestLoadRequiresReason(t *testing.T) {
	_, err := Load(writePolicy(t, `{"suppress": [{"id": "F-1"}]}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reason is required")
}

func TestLoadRejectsBadExpiryAndFailOn(t *testing.T) {
	_, err := Load(writePolicy(t, `{"suppress": [{"id": "F-1", "reason": "x", "expiry": "next-tuesday"}]}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "YYYY-MM-DD")

	_, err = Load(writePolicy(t, `{"fail_on": "sometimes"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid")
}

func TestApplySuppressesExactAndPrefix(t *testing.T) {
	p := &Policy{Suppress: []Suppression{
		{ID: "SCA-CVE-2021-1234", Reason: "accepted"},
		{ID: "POISON-006-*", Reason: "intentional name"},
	}}
	findings := []verdict.Finding{
		{ID: "SCA-CVE-2021-1234"},
		{ID: "POISON-006-mcp_json-gitt"},
		{ID: "F-1"}, // not suppressed
	}
	kept, suppressed := p.Apply(findings, time.Now())
	assert.Len(t, suppressed, 2)
	require.Len(t, kept, 1)
	assert.Equal(t, "F-1", kept[0].ID)
}

func TestApplyExpiredSuppressionResurfaces(t *testing.T) {
	p := &Policy{Suppress: []Suppression{
		{ID: "F-1", Reason: "temp accept", Expiry: "2020-01-01"},
	}}
	findings := []verdict.Finding{{ID: "F-1"}}
	// "now" is well after the expiry → suppression no longer applies.
	kept, suppressed := p.Apply(findings, time.Date(2026, 5, 29, 0, 0, 0, 0, time.UTC))
	assert.Empty(t, suppressed, "expired suppression must not silence the finding")
	require.Len(t, kept, 1)
}

func TestApplyActiveExpirySuppresses(t *testing.T) {
	p := &Policy{Suppress: []Suppression{
		{ID: "F-1", Reason: "temp accept", Expiry: "2099-01-01"},
	}}
	kept, suppressed := p.Apply([]verdict.Finding{{ID: "F-1"}}, time.Now())
	assert.Len(t, suppressed, 1)
	assert.Empty(t, kept)
}

func TestNilPolicyIsSafe(t *testing.T) {
	var p *Policy
	kept, suppressed := p.Apply([]verdict.Finding{{ID: "F-1"}}, time.Now())
	assert.Len(t, kept, 1)
	assert.Empty(t, suppressed)
	assert.False(t, p.IsAllowlisted("/x/y"))
	assert.Empty(t, p.DeniedCategoryHits([]verdict.Finding{{Category: "exfiltration"}}))
}

func TestIsAllowlisted(t *testing.T) {
	p := &Policy{Allowlist: []string{"trusted-plugin", "/abs/path/to/other"}}
	assert.True(t, p.IsAllowlisted("/some/dir/trusted-plugin"))
	assert.True(t, p.IsAllowlisted("/abs/path/to/other"))
	assert.False(t, p.IsAllowlisted("/some/dir/untrusted"))
}

func TestDeniedCategoryHits(t *testing.T) {
	p := &Policy{DenyCategories: []string{"exfiltration", "hook_abuse"}}
	hits := p.DeniedCategoryHits([]verdict.Finding{
		{ID: "F-1", Category: "exfiltration"},
		{ID: "F-2", Category: "overscope"},
		{ID: "F-3", Category: "hook_abuse"},
	})
	require.Len(t, hits, 2)
}

func TestResolvePrefersExplicitThenCwd(t *testing.T) {
	cwd := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cwd, DefaultFileName), []byte("{}"), 0o600))
	assert.Equal(t, "/explicit/path.json", Resolve("/explicit/path.json", cwd))
	assert.Equal(t, filepath.Join(cwd, DefaultFileName), Resolve("", cwd))
	assert.Equal(t, "", Resolve("", t.TempDir())) // empty dir → no policy
}
