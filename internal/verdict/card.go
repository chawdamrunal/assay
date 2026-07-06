package verdict

import (
	"fmt"
	"sort"
	"strings"
)

// CardOptions carries render-context the Verdict itself does not hold.
type CardOptions struct {
	// DroppedCount is how many findings the citation validator removed. Shown
	// in the trust footer for LLM-tier scans; pass 0 for --no-llm.
	DroppedCount int
	// ReportURL, when non-empty, becomes the "full report" link target.
	ReportURL string
}

var cardSeverityRank = map[string]int{"critical": 0, "high": 1, "medium": 2, "low": 3, "info": 4}

// cardRank returns the sort rank for a severity string, case-insensitively.
// Unknown/miscased severities must not collide with the zero-value rank
// ("critical"'s rank is 0, same as a missing map key), so they fall through
// to a rank past the end of the known table — sorting last, not first.
func cardRank(sev string) int {
	if r, ok := cardSeverityRank[strings.ToLower(sev)]; ok {
		return r
	}
	return len(cardSeverityRank) // unknown severity sorts after all known ones
}

// mdInline sanitizes an artifact-controlled string before it is interpolated
// into the markdown card. Findings' titles, evidence file paths, the target
// name, and free-text summaries all originate from the scanned artifact (or,
// in LLM mode, from model output derived from it) and are rendered verbatim
// into a PR comment. Without this, a crafted string containing a newline
// could break out of its line and inject a spoofed markdown block — e.g. a
// fake "### ✅ Assay: safe" header. Newlines/tabs/control characters collapse
// to a space and backticks are neutralized so nothing can escape the single
// line it was meant to occupy.
func mdInline(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' || r < 0x20 {
			return ' '
		}
		return r
	}, s)
	s = strings.ReplaceAll(s, "`", "'")
	return strings.TrimSpace(s)
}

func verdictBadge(v string) string {
	switch v {
	case "unsafe":
		return "⛔"
	case "caution":
		return "⚠️"
	default:
		return "✅"
	}
}

// RenderCard renders a Verdict as a skimmable GitHub-flavored-markdown card,
// used identically by the CLI (terminal) and the GitHub Action (PR comment).
// Pure: no I/O, no time, no network — golden-file testable.
func RenderCard(v Verdict, opts CardOptions) string {
	var b strings.Builder
	fmt.Fprintf(&b, "### %s Assay: %s — %s\n", verdictBadge(v.Verdict), v.Verdict, mdInline(v.Target.Name))
	if s := mdInline(v.Summary); s != "" {
		fmt.Fprintf(&b, "%s\n\n", s)
	}
	if c := mdInline(firstLine(v.ClaimsVsReality)); c != "" {
		fmt.Fprintf(&b, "Claims vs reality: %s\n\n", c)
	}

	findings := append([]Finding(nil), v.Findings...)
	sort.SliceStable(findings, func(i, j int) bool {
		return cardRank(findings[i].Severity) < cardRank(findings[j].Severity)
	})
	for _, f := range findings {
		loc := ""
		if len(f.Evidence) > 0 {
			loc = fmt.Sprintf("  %s:%d", mdInline(f.Evidence[0].File), f.Evidence[0].Line)
		}
		tag := ""
		switch f.Source {
		case SourceSCA, SourcePoison, SourceSecret:
			tag = "  [floor]"
		}
		fmt.Fprintf(&b, "%-9s %s%s%s\n", strings.ToUpper(f.Severity), mdInline(f.Title), loc, tag)
	}
	if len(findings) == 0 {
		b.WriteString("No findings.\n")
	}

	b.WriteString("—\n")
	// llmTier is false for deterministic-only scans (--no-llm, or any run that
	// never set a prompt version): no citation validator ran, so there is
	// nothing to have dropped and no prompt to report. Both footer segments
	// below are gated on it so a no-llm card can never claim findings were
	// "dropped as unverified" by a validation pass that didn't happen.
	llmTier := v.Scanner.PromptVersion != "" && v.Scanner.PromptVersion != PromptVersionNoLLM
	foot := fmt.Sprintf("%d findings shown", len(findings))
	if llmTier && opts.DroppedCount > 0 {
		foot += fmt.Sprintf(" · %d dropped as unverified (hallucination guard)", opts.DroppedCount)
	}
	if llmTier {
		foot += " · prompt " + v.Scanner.PromptVersion
	}
	if opts.ReportURL != "" {
		foot += fmt.Sprintf(" · [full report ↗](%s)", opts.ReportURL)
	}
	b.WriteString(foot + "\n")
	return b.String()
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}
