package verdict

import (
	"fmt"
	"strings"
)

// Diff annotates a new scan's findings relative to a prior scan of the same
// target. Returns:
//   - annotated: every finding from `current`, each carrying a populated
//     Finding.Diff with status "new" | "stable" | "changed" and (when matched)
//     the prior finding's ID
//   - resolved: findings present in `prior` that have no match in `current`
//
// Matching is done by a stable composite key (category + title +
// first-evidence file:line). Title alone is too loose (Claude sometimes
// rephrases); evidence alone misses category context. The composite is
// stable as long as the underlying code path doesn't move.
//
// Material-change detection (for "changed" status): severity, evidence-count,
// or description differ between the two findings.
func Diff(prior, current []Finding) (annotated, resolved []Finding) {
	priorByKey := make(map[string]Finding, len(prior))
	for _, f := range prior {
		priorByKey[findingKey(f)] = f
	}
	used := make(map[string]bool, len(prior))

	annotated = make([]Finding, 0, len(current))
	for _, c := range current {
		key := findingKey(c)
		anno := &DiffAnnotation{}
		if p, ok := priorByKey[key]; ok {
			used[key] = true
			anno.PriorID = p.ID
			if findingsMateriallyDiffer(p, c) {
				anno.Status = "changed"
			} else {
				anno.Status = "stable"
			}
		} else {
			anno.Status = "new"
		}
		out := c
		out.Diff = anno
		annotated = append(annotated, out)
	}

	resolved = make([]Finding, 0)
	for _, p := range prior {
		if !used[findingKey(p)] {
			out := p
			out.Diff = &DiffAnnotation{Status: "resolved", PriorID: p.ID}
			resolved = append(resolved, out)
		}
	}
	return annotated, resolved
}

// findingKey is the stable identity for matching findings across scans.
// Format: "<category>|<title>|<file>:<line>" (first evidence row). Lower-
// cased and trimmed so trivial whitespace / case drift don't break the match.
func findingKey(f Finding) string {
	loc := ""
	if len(f.Evidence) > 0 {
		loc = fmt.Sprintf("%s:%d", strings.TrimSpace(f.Evidence[0].File), f.Evidence[0].Line)
	}
	return strings.ToLower(strings.TrimSpace(f.Category)) +
		"|" + strings.ToLower(strings.TrimSpace(f.Title)) +
		"|" + strings.ToLower(loc)
}

// findingsMateriallyDiffer returns true if a reviewer should care about the
// change between two findings that share the same identity key. The three
// fields here (severity, evidence count, description) are the ones a security
// reader actually inspects on an update.
func findingsMateriallyDiffer(a, b Finding) bool {
	if !strings.EqualFold(a.Severity, b.Severity) {
		return true
	}
	if len(a.Evidence) != len(b.Evidence) {
		return true
	}
	if strings.TrimSpace(a.Description) != strings.TrimSpace(b.Description) {
		return true
	}
	return false
}
