package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"version"})

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "assay") {
		t.Errorf("expected output to contain 'assay', got: %q", output)
	}
	if !strings.Contains(output, version) {
		t.Errorf("expected output to contain version %q, got: %q", version, output)
	}
}
