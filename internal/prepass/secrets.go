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

// secretRule is a labelled regex used to detect a known credential format.
type secretRule struct {
	name     string
	pattern  *regexp.Regexp
	message  string
	severity string
}

// secretRules is intentionally short and high-precision. Generic high-entropy
// detection has too many false positives; we prefer named, well-known formats.
var secretRules = []secretRule{
	{
		name:     "aws-access-key-id",
		pattern:  regexp.MustCompile(`\b(AKIA|ASIA)[0-9A-Z]{16}\b`),
		message:  "AWS Access Key ID pattern",
		severity: "high",
	},
	{
		name:     "anthropic-api-key",
		pattern:  regexp.MustCompile(`\bsk-ant-[a-zA-Z0-9_-]{20,}\b`),
		message:  "Anthropic API key pattern",
		severity: "critical",
	},
	{
		name:     "openai-api-key",
		pattern:  regexp.MustCompile(`\bsk-[a-zA-Z0-9]{20,}\b`),
		message:  "OpenAI API key pattern (also matches some generic sk- prefixes)",
		severity: "high",
	},
	{
		name:     "github-token",
		pattern:  regexp.MustCompile(`\b(ghp|gho|ghu|ghs|ghr)_[a-zA-Z0-9]{30,}\b`),
		message:  "GitHub token pattern",
		severity: "critical",
	},
	{
		name:     "slack-token",
		pattern:  regexp.MustCompile(`\bxox[abprs]-[a-zA-Z0-9-]{10,}\b`),
		message:  "Slack token pattern",
		severity: "high",
	},
	{
		name:     "private-key-block",
		pattern:  regexp.MustCompile(`-----BEGIN (RSA|DSA|EC|OPENSSH|PGP) PRIVATE KEY-----`),
		message:  "Private key block",
		severity: "critical",
	},
}

// ScanSecrets walks the directory tree under root and returns Hits for any
// secret patterns found. Hidden directories (.git, .vscode, etc.) and files
// larger than opts.MaxFileSize are skipped.
func ScanSecrets(root string, opts Options) ([]Hit, error) {
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
		if strings.HasPrefix(base, ".") && d.IsDir() && path != root {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasPrefix(base, ".") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > maxSize {
			return nil
		}

		fileHits, err := scanSecretsInFile(path)
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

func scanSecretsInFile(path string) ([]Hit, error) {
	f, err := os.Open(path) // #nosec G304 -- path is under root from WalkDir
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
		for _, rule := range secretRules {
			if loc := rule.pattern.FindStringIndex(text); loc != nil {
				hits = append(hits, Hit{
					Category: "secret",
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
