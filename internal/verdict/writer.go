package verdict

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Write persists the scan artifacts under dir:
//
//	audit.json          — schema-validated machine-readable verdict
//	audit.md            — human-readable report (synthesis output)
//	investigation.log   — full agent trace for reproducibility
//
// dir must already exist (typically allocated by store.History.Allocate).
func Write(dir string, v Verdict, auditMarkdown, investigationLog string) error {
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("verdict.Write: dir not accessible: %w", err)
	}

	jsonBody, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("verdict.Write: marshal: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "audit.json"), jsonBody, 0o600); err != nil {
		return fmt.Errorf("verdict.Write audit.json: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "audit.md"), []byte(auditMarkdown), 0o600); err != nil {
		return fmt.Errorf("verdict.Write audit.md: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "investigation.log"), []byte(investigationLog), 0o600); err != nil {
		return fmt.Errorf("verdict.Write investigation.log: %w", err)
	}
	return nil
}

// Read parses an audit.json at path back into a Verdict.
func Read(path string) (Verdict, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- audit.json under a known scan dir
	if err != nil {
		return Verdict{}, fmt.Errorf("verdict.Read: %w", err)
	}
	var v Verdict
	if err := json.Unmarshal(data, &v); err != nil {
		return Verdict{}, fmt.Errorf("verdict.Read parse: %w", err)
	}
	return v, nil
}
