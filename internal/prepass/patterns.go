package prepass

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type patternRule struct {
	name     string
	pattern  *regexp.Regexp
	message  string
	severity string
}

// patternRules detect code constructs that warrant a closer look from Sonnet.
// They are NOT verdicts — they are evidence for Stage 0 triage.
var patternRules = []patternRule{
	{
		name:     "js-eval",
		pattern:  regexp.MustCompile(`\beval\s*\(`),
		message:  "Dynamic code evaluation via eval()",
		severity: "info",
	},
	{
		name:     "js-function-constructor",
		pattern:  regexp.MustCompile(`\bnew\s+Function\s*\(`),
		message:  "Dynamic code construction via new Function()",
		severity: "info",
	},
	{
		name:     "js-child-process",
		pattern:  regexp.MustCompile(`require\s*\(\s*['"]child_process['"]\s*\)`),
		message:  "Shell command execution via child_process",
		severity: "info",
	},
	{
		name:     "js-vm-run",
		pattern:  regexp.MustCompile(`\b(vm)\.(runInContext|runInNewContext|runInThisContext)\b`),
		message:  "Node vm sandbox execution",
		severity: "info",
	},
	{
		name:     "py-exec-or-eval",
		pattern:  regexp.MustCompile(`\b(exec|eval)\s*\(`),
		message:  "Python exec/eval — dynamic code",
		severity: "info",
	},
	{
		name:     "py-subprocess",
		pattern:  regexp.MustCompile(`\b(subprocess\.(Popen|run|call|check_output)|os\.system|os\.popen)\s*\(`),
		message:  "Python shell execution",
		severity: "info",
	},
	{
		name:     "go-exec-command",
		pattern:  regexp.MustCompile(`\bexec\.(Command|CommandContext)\s*\(`),
		message:  "Go shell execution via os/exec",
		severity: "info",
	},
	{
		name:     "sensitive-path-read",
		pattern:  regexp.MustCompile(`['"](~?/\.(ssh|aws|gnupg|config/gcloud|kube)/[^'"]*)`),
		message:  "Read of credential-bearing dotfile path",
		severity: "high",
	},
	{
		// Catches the constructed form used to dodge the inlined-path regex
		// above: e.g. `path.join(os.homedir(), '.aws', 'credentials')` or
		// `os.homedir() + '/.ssh/id_rsa'`. The QuickProfile relies on this
		// to upgrade "cosmetic plugin that touches .aws/" to high-severity.
		name:     "sensitive-path-constructed",
		pattern:  regexp.MustCompile(`(?:homedir|HOME|expanduser|user\.HomeDir)\b[^'"]{0,40}['"]\.?(ssh|aws|gnupg|kube)\b`),
		message:  "Constructed path to credential-bearing dotfile dir (homedir + .aws/.ssh/...)",
		severity: "high",
	},
	{
		name:     "dotenv-read",
		pattern:  regexp.MustCompile(`['"](~?/?\.env(\.[a-z]+)?)['"]`),
		message:  "Read of .env file (may contain secrets)",
		severity: "medium",
	},
	{
		name:     "outbound-http",
		pattern:  regexp.MustCompile(`\b(fetch|axios\.(get|post|put|delete|request)|http\.(get|post|request)|requests\.(get|post|put|delete))\s*\(`),
		message:  "Outbound HTTP call",
		severity: "info",
	},
	{
		name:     "base64-blob",
		pattern:  regexp.MustCompile(`\b[A-Za-z0-9+/]{120,}={0,2}\b`),
		message:  "Long base64-looking blob — could be obfuscated payload",
		severity: "low",
	},
}

// ScanPatterns walks root and returns Hits for any suspicious code patterns.
func ScanPatterns(root string, opts Options) ([]Hit, error) {
	maxSize := opts.MaxFileSize
	if maxSize == 0 {
		maxSize = DefaultMaxFileSize
	}
	var hits []Hit
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		base := filepath.Base(path)
		if d.IsDir() && strings.HasPrefix(base, ".") {
			return filepath.SkipDir
		}
		if d.IsDir() || strings.HasPrefix(base, ".") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > maxSize {
			return nil
		}
		fileHits, err := scanPatternsInFile(path)
		if err != nil {
			return err
		}
		hits = append(hits, fileHits...)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", root, err)
	}
	return hits, nil
}

func scanPatternsInFile(path string) ([]Hit, error) {
	f, err := os.Open(path) // #nosec G304 -- bounded under WalkDir root
	if errors.Is(err, fs.ErrPermission) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	var hits []Hit
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	line := 0
	for scanner.Scan() {
		line++
		text := scanner.Text()
		for _, rule := range patternRules {
			if loc := rule.pattern.FindStringIndex(text); loc != nil {
				hits = append(hits, Hit{
					Category: "pattern",
					Severity: rule.severity,
					File:     path,
					Line:     line,
					Snippet:  text[loc[0]:loc[1]],
					Message:  rule.message,
					Metadata: map[string]string{"rule": rule.name},
				})
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return hits, fmt.Errorf("scan %s: %w", path, err)
	}
	return hits, nil
}
