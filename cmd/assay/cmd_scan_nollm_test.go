package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/chawdamrunal/assay/internal/claude"
	"github.com/chawdamrunal/assay/internal/prepass"
	"github.com/chawdamrunal/assay/internal/verdict"
)

func TestSecretsToFindings(t *testing.T) {
	root := "/tmp/scan-root"
	hits := []prepass.Hit{
		{Category: "secret", Severity: "critical", File: filepath.Join(root, "a/keys.env"), Line: 3, Snippet: "sk-ant-xxxx", Message: "Anthropic API key pattern"},
		{Category: "secret", Severity: "high", File: filepath.Join(root, "c/config.yaml"), Line: 42, Snippet: "AKIAABCDEFGHIJKLMNOP", Message: "AWS access key pattern"},
		{Category: "pattern", Severity: "high", File: filepath.Join(root, "b.js"), Line: 1, Snippet: "eval(x)", Message: "eval"}, // must be ignored
	}
	got := secretsToFindings(hits, root)
	if len(got) != 2 {
		t.Fatalf("want 2 secret findings, got %d", len(got))
	}

	f0 := got[0]
	if f0.ID != "SECRET-a/keys.env-L3" {
		t.Fatalf("want ID=%q, got %q", "SECRET-a/keys.env-L3", f0.ID)
	}
	if f0.Category != "secret" {
		t.Fatalf("want Category=%q, got %q", "secret", f0.Category)
	}
	if f0.Source != verdict.SourceSecret {
		t.Fatalf("want Source=%q, got %q", verdict.SourceSecret, f0.Source)
	}
	if len(f0.Evidence) != 1 || f0.Evidence[0].File != "a/keys.env" || f0.Evidence[0].Line != 3 {
		t.Fatalf("evidence should be target-relative file:line, got %+v", f0.Evidence)
	}
	if f0.Severity != "critical" {
		t.Fatalf("severity should carry through, got %q", f0.Severity)
	}
	if f0.Title != "Anthropic API key pattern" {
		t.Fatalf("title should carry through from hit Message, got %q", f0.Title)
	}

	f1 := got[1]
	if f1.ID != "SECRET-c/config.yaml-L42" {
		t.Fatalf("want ID=%q, got %q", "SECRET-c/config.yaml-L42", f1.ID)
	}
	if f1.Category != "secret" {
		t.Fatalf("want Category=%q, got %q", "secret", f1.Category)
	}
	if f1.Source != verdict.SourceSecret {
		t.Fatalf("want Source=%q, got %q", verdict.SourceSecret, f1.Source)
	}
	if len(f1.Evidence) != 1 || f1.Evidence[0].File != "c/config.yaml" || f1.Evidence[0].Line != 42 {
		t.Fatalf("evidence should be target-relative file:line, got %+v", f1.Evidence)
	}
	if f1.Severity != "high" {
		t.Fatalf("severity should carry through, got %q", f1.Severity)
	}
}

func TestNoLLMScan_FlagsUnsafeOnPlantedSecret_NoClient(t *testing.T) {
	// Guard: --no-llm must never construct a Claude client.
	scanClientFactory = func(_ context.Context) (claude.Client, error) {
		t.Fatal("--no-llm must not construct a Claude client")
		return nil, nil
	}
	t.Cleanup(func() { scanClientFactory = nil })

	dir := t.TempDir()
	// AKIAIOSFODNN7EXAMPLE is AWS's own documented example key (not a real secret).
	if err := os.WriteFile(filepath.Join(dir, "config.txt"), []byte("aws_key=AKIAIOSFODNN7EXAMPLE\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	outputDir := t.TempDir()
	cmd := newScanCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{dir, "--no-llm", "--offline", "--format", "sarif", "--output", outputDir})

	err := cmd.Execute()
	// Unsafe verdict (critical/high secret) trips the default --fail-on=unsafe → exit 2.
	var ec *exitCodeError
	if err == nil || !errors.As(err, &ec) || ec.code != 2 {
		t.Fatalf("want exitCodeError{code:2}, got %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte("Assay: unsafe")) {
		t.Fatalf("expected card with unsafe verdict, got:\n%s", out.String())
	}
	if _, err := os.Stat(filepath.Join(outputDir, "audit.sarif")); err != nil {
		t.Fatalf("want audit.sarif written to --output dir, got: %v", err)
	}
}

// TestNoLLMScan_PolicyFailOnOverridesDefaultWhenFlagNotSet proves the
// --no-llm gate honors the documented precedence: --fail-on flag > policy
// fail_on > default (unsafe). With no --fail-on flag and a policy pinning
// fail_on to "off", the unsafe verdict from the planted secret must NOT gate
// the exit code.
func TestNoLLMScan_PolicyFailOnOverridesDefaultWhenFlagNotSet(t *testing.T) {
	// Guard: --no-llm must never construct a Claude client.
	scanClientFactory = func(_ context.Context) (claude.Client, error) {
		t.Fatal("--no-llm must not construct a Claude client")
		return nil, nil
	}
	t.Cleanup(func() { scanClientFactory = nil })

	dir := t.TempDir()
	// AKIAIOSFODNN7EXAMPLE is AWS's own documented example key (not a real secret).
	if err := os.WriteFile(filepath.Join(dir, "config.txt"), []byte("aws_key=AKIAIOSFODNN7EXAMPLE\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	policyPath := filepath.Join(t.TempDir(), ".assay-policy.json")
	if err := os.WriteFile(policyPath, []byte(`{"fail_on":"off"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	cmd := newScanCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	// No --fail-on flag: policy.fail_on="off" must override the "unsafe" default.
	cmd.SetArgs([]string{dir, "--no-llm", "--offline", "--policy", policyPath})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("want exit 0 (policy fail_on=off overrides default --fail-on=unsafe), got %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte("Assay: unsafe")) {
		t.Fatalf("expected card with unsafe verdict (fail_on=off gates exit code only, not the verdict label), got:\n%s", out.String())
	}
}
