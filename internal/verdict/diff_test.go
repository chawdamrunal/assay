package verdict

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// finding is a tiny test helper to keep the table cases readable.
func finding(id, sev, cat, title, file string, line int, desc string) Finding {
	return Finding{
		ID:          id,
		Severity:    sev,
		Category:    cat,
		Title:       title,
		Description: desc,
		Evidence:    []Evidence{{File: file, Line: line, Snippet: "..."}},
	}
}

func TestDiffMarksNewStableChangedResolved(t *testing.T) {
	prior := []Finding{
		finding("P1", "critical", "exfil", "Reads ~/.aws/credentials", "src/main.js", 23, "Reads creds."),
		finding("P2", "high", "overscope", "Outbound POST to attacker host", "src/main.js", 10, "POSTs to attacker."),
		finding("P3", "medium", "supply_chain", "Typosquat of rainbow-formatter", "plugin.json", 2, "Lookalike name."),
	}
	current := []Finding{
		// Stable: same key + same severity/evidence/description as P1.
		finding("C1", "critical", "exfil", "Reads ~/.aws/credentials", "src/main.js", 23, "Reads creds."),
		// Changed: same key as P2 but severity escalated AND description rewritten.
		finding("C2", "critical", "overscope", "Outbound POST to attacker host", "src/main.js", 10, "POSTs creds to attacker; previously misdiagnosed as overscope-only."),
		// New: key not in prior.
		finding("C3", "low", "ux", "Console.log of input on every call", "src/main.js", 31, "Noisy console.log leaks."),
		// P3 is resolved (absent in current).
	}

	annotated, resolved := Diff(prior, current)
	require.Len(t, annotated, 3)
	require.Len(t, resolved, 1)

	byID := map[string]Finding{}
	for _, f := range annotated {
		byID[f.ID] = f
	}

	require.NotNil(t, byID["C1"].Diff)
	assert.Equal(t, "stable", byID["C1"].Diff.Status)
	assert.Equal(t, "P1", byID["C1"].Diff.PriorID)

	require.NotNil(t, byID["C2"].Diff)
	assert.Equal(t, "changed", byID["C2"].Diff.Status, "severity + description drift should trigger changed")
	assert.Equal(t, "P2", byID["C2"].Diff.PriorID)

	require.NotNil(t, byID["C3"].Diff)
	assert.Equal(t, "new", byID["C3"].Diff.Status)
	assert.Empty(t, byID["C3"].Diff.PriorID, "new findings have no prior_id")

	require.NotNil(t, resolved[0].Diff)
	assert.Equal(t, "resolved", resolved[0].Diff.Status)
	assert.Equal(t, "P3", resolved[0].ID, "preserves prior ID")
	assert.Equal(t, "P3", resolved[0].Diff.PriorID)
}

func TestDiffKeyIgnoresCaseAndWhitespace(t *testing.T) {
	prior := []Finding{finding("P1", "high", "Exfil", " Reads creds ", "src/main.js", 23, "x")}
	current := []Finding{finding("C1", "high", "EXFIL", "reads creds", "src/main.js", 23, "x")}
	annotated, resolved := Diff(prior, current)
	require.Len(t, annotated, 1)
	require.Empty(t, resolved, "case/whitespace shouldn't break match")
	assert.Equal(t, "stable", annotated[0].Diff.Status)
}

func TestDiffSeverityChangeAloneIsChanged(t *testing.T) {
	prior := []Finding{finding("P1", "medium", "exfil", "X", "f.js", 1, "same")}
	current := []Finding{finding("C1", "high", "exfil", "X", "f.js", 1, "same")}
	annotated, _ := Diff(prior, current)
	require.Len(t, annotated, 1)
	assert.Equal(t, "changed", annotated[0].Diff.Status)
}

func TestDiffEvidenceCountChangeIsChanged(t *testing.T) {
	prior := []Finding{{ID: "P1", Severity: "high", Category: "x", Title: "T",
		Evidence: []Evidence{{File: "f.js", Line: 1, Snippet: "a"}}}}
	current := []Finding{{ID: "C1", Severity: "high", Category: "x", Title: "T",
		Evidence: []Evidence{{File: "f.js", Line: 1, Snippet: "a"}, {File: "f.js", Line: 2, Snippet: "b"}}}}
	annotated, _ := Diff(prior, current)
	require.Len(t, annotated, 1)
	assert.Equal(t, "changed", annotated[0].Diff.Status)
}

func TestDiffEmptyPriorMakesEverythingNew(t *testing.T) {
	current := []Finding{finding("C1", "high", "x", "T", "f.js", 1, "d")}
	annotated, resolved := Diff(nil, current)
	require.Len(t, annotated, 1)
	assert.Equal(t, "new", annotated[0].Diff.Status)
	assert.Empty(t, resolved)
}

func TestDiffEmptyCurrentMakesEverythingResolved(t *testing.T) {
	prior := []Finding{finding("P1", "high", "x", "T", "f.js", 1, "d")}
	annotated, resolved := Diff(prior, nil)
	assert.Empty(t, annotated)
	require.Len(t, resolved, 1)
	assert.Equal(t, "resolved", resolved[0].Diff.Status)
}

func TestDiffNoEvidenceUsesEmptyLocation(t *testing.T) {
	prior := []Finding{{ID: "P1", Severity: "low", Category: "x", Title: "T"}}
	current := []Finding{{ID: "C1", Severity: "low", Category: "x", Title: "T"}}
	annotated, resolved := Diff(prior, current)
	require.Len(t, annotated, 1)
	require.Empty(t, resolved)
	assert.Equal(t, "stable", annotated[0].Diff.Status, "findings with no evidence still match by (category,title)")
}
