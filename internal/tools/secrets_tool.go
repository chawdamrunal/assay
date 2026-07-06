package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/chawdamrunal/assay/internal/prepass"
)

// SecretsTool exposes prepass.ScanSecrets as an agent-callable tool, bounded to a root.
type SecretsTool struct {
	root string
}

// NewSecretsTool returns a SecretsTool rooted at root.
func NewSecretsTool(root string) *SecretsTool {
	abs, _ := filepath.Abs(root)
	return &SecretsTool{root: abs}
}

// Def returns the tool definition.
func (s *SecretsTool) Def() Tool {
	return Tool{
		Name:        "secret_scan",
		Description: "Scan a sub-path under the target root for known secret patterns (AWS, GitHub, Anthropic, OpenAI, Slack, PEM private keys). Returns matches as JSON.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "Sub-path relative to root, e.g. \".\" or \"src/\""},
			},
			"required": []string{"path"},
		},
	}
}

// Scan executes the tool.
func (s *SecretsTool) Scan(_ context.Context, in Invocation) (Result, error) {
	relPath, _ := in.Input["path"].(string)
	if relPath == "" {
		relPath = "."
	}
	full, err := s.resolve(relPath)
	if err != nil {
		return Result{}, err
	}
	hits, err := prepass.ScanSecrets(full, prepass.Options{})
	if err != nil {
		return Result{}, fmt.Errorf("secret_scan: %w", err)
	}
	if len(hits) == 0 {
		return Result{Text: "(no secrets found)"}, nil
	}
	out, _ := json.MarshalIndent(hits, "", "  ")
	return Result{Text: string(out)}, nil
}

func (s *SecretsTool) resolve(rel string) (string, error) {
	clean := filepath.Clean(rel)
	if filepath.IsAbs(clean) {
		return "", errors.New("absolute paths not allowed")
	}
	full := filepath.Join(s.root, clean)
	abs, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(abs+string(filepath.Separator), s.root+string(filepath.Separator)) && abs != s.root {
		return "", fmt.Errorf("path escapes root: %s", rel)
	}
	return abs, nil
}
