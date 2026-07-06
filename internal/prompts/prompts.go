// Package prompts exposes versioned prompt templates for the scanner.
// Each version lives in its own subdirectory (v1, v2, ...). Production
// code references prompts by name; the verdict JSON records which version
// produced it.
package prompts

import (
	"embed"
	"fmt"
)

//go:embed v1/*.md
var v1FS embed.FS

// Version is the current prompt set version.
const Version = "v1"

// Load returns the named prompt's text for the given version.
// Available names: triage, claims, threat_model, investigator, exploitability, synthesis.
func Load(version, name string) (string, error) {
	var fs embed.FS
	switch version {
	case "v1":
		fs = v1FS
	default:
		return "", fmt.Errorf("unknown prompt version: %s", version)
	}
	data, err := fs.ReadFile(version + "/" + name + ".md")
	if err != nil {
		return "", fmt.Errorf("prompt %s/%s: %w", version, name, err)
	}
	return string(data), nil
}
