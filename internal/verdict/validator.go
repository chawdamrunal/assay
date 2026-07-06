package verdict

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// DroppedFinding is a finding that failed citation validation, recorded for
// the investigation.log so the user can see what the LLM tried to claim.
type DroppedFinding struct {
	ID       string
	Severity string
	Title    string
	Reason   string
}

// LineWindow is the ±N line tolerance when looking for a quoted snippet.
// Forgives off-by-one errors from the agent without weakening confabulation defense.
const LineWindow = 3

// Validate re-reads every evidence citation in findings under root and drops
// findings whose snippets don't appear in the file at (or near) the cited line.
// info-severity findings with no evidence are kept as-is (they represent
// "no issues found" reports from investigators).
func Validate(root string, findings []Finding) (kept []Finding, dropped []DroppedFinding) {
	// Cache file contents to avoid re-reading the same file many times.
	cache := map[string][]string{}
	loadFile := func(file string) ([]string, error) {
		if lines, ok := cache[file]; ok {
			return lines, nil
		}
		full := filepath.Join(root, file)
		data, err := os.ReadFile(full) // #nosec G304 -- root-bounded scan dir
		if err != nil {
			return nil, err
		}
		lines := strings.Split(string(data), "\n")
		cache[file] = lines
		return lines, nil
	}

	for _, f := range findings {
		// Deterministic-floor findings (SCA, poison) carry synthetic evidence
		// — a manifest reference, not a source-code citation — so the
		// file-content re-read does not apply. Keep them as-is. Gating on
		// Source makes the exemption explicit and auditable instead of relying
		// on a synthetic snippet happening to match a real file.
		if f.Source != "" && f.Source != SourceLLM {
			kept = append(kept, f)
			continue
		}

		// info-severity with no evidence: keep as-is.
		if f.Severity == "info" && len(f.Evidence) == 0 {
			kept = append(kept, f)
			continue
		}

		// Validate each evidence entry; keep only the ones that match.
		var validated []Evidence
		var lastReason string
		for _, e := range f.Evidence {
			lines, err := loadFile(e.File)
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					lastReason = fmt.Sprintf("file not found: %s", e.File)
				} else {
					lastReason = fmt.Sprintf("file read error %s: %v", e.File, err)
				}
				continue
			}
			if snippetFound(lines, e.Line, e.Snippet) {
				validated = append(validated, e)
			} else {
				lastReason = fmt.Sprintf("snippet not found in %s near line %d: %q", e.File, e.Line, truncateSnippet(e.Snippet, 80))
			}
		}

		if len(validated) > 0 {
			fkept := f
			fkept.Evidence = validated
			kept = append(kept, fkept)
		} else {
			dropped = append(dropped, DroppedFinding{
				ID:       f.ID,
				Severity: f.Severity,
				Title:    f.Title,
				Reason:   lastReason,
			})
		}
	}
	return kept, dropped
}

// snippetFound checks for the normalized snippet within ±LineWindow lines of cited.
// Normalization: trim leading/trailing whitespace per line; case-sensitive content match.
func snippetFound(lines []string, cited int, snippet string) bool {
	needle := normalizeWS(snippet)
	if needle == "" {
		return false
	}
	// Build a window around the cited line (1-indexed).
	start := cited - LineWindow
	if start < 1 {
		start = 1
	}
	end := cited + LineWindow
	if end > len(lines) {
		end = len(lines)
	}

	// For multi-line snippets, join the window lines (whitespace-normalized)
	// and check substring. For single-line snippets, check each line.
	if strings.Contains(snippet, "\n") {
		windowNorm := ""
		for i := start - 1; i < end; i++ {
			windowNorm += normalizeWS(lines[i]) + "\n"
		}
		return strings.Contains(windowNorm, needle)
	}

	for i := start - 1; i < end; i++ {
		if strings.Contains(normalizeWS(lines[i]), needle) {
			return true
		}
	}
	return false
}

// normalizeWS collapses runs of whitespace within a string to single spaces,
// and trims edges. Comparison is then substring-based.
func normalizeWS(s string) string {
	var b strings.Builder
	inSpace := false
	for _, r := range s {
		switch r {
		case ' ', '\t':
			if !inSpace && b.Len() > 0 {
				b.WriteByte(' ')
			}
			inSpace = true
		case '\n', '\r':
			// preserve newline boundaries for multi-line matching; surrounding
			// whitespace is implicitly dropped by resetting inSpace here.
			inSpace = false
			b.WriteRune(r)
		default:
			b.WriteRune(r)
			inSpace = false
		}
	}
	return strings.TrimSpace(b.String())
}

func truncateSnippet(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
