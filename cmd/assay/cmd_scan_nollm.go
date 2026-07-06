package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/chawdamrunal/assay/internal/floor"
	"github.com/chawdamrunal/assay/internal/inventory"
	"github.com/chawdamrunal/assay/internal/policy"
	"github.com/chawdamrunal/assay/internal/prepass"
	"github.com/chawdamrunal/assay/internal/store"
	"github.com/chawdamrunal/assay/internal/verdict"
)

// secretsToFindings promotes high-precision prepass secret Hits into verdict
// Findings for the deterministic --no-llm tier. Only "secret"-category hits are
// promoted; suspicious-pattern hits stay hints (they need LLM confirmation).
// Evidence is made target-relative and is verbatim from the file, so these
// survive validation without the citation re-read even if it were run.
func secretsToFindings(hits []prepass.Hit, targetRoot string) []verdict.Finding {
	out := make([]verdict.Finding, 0, len(hits))
	for _, h := range hits {
		if h.Category != "secret" {
			continue
		}
		rel := h.File
		// The fallback to the absolute h.File is effectively unreachable in
		// practice: both targetRoot and h.File are absolute paths under the
		// same root (prepass.ScanSecrets walks targetRoot via
		// filepath.WalkDir), so this exists only for safety.
		if r, err := filepath.Rel(targetRoot, h.File); err == nil {
			rel = r
		}
		out = append(out, verdict.Finding{
			ID:       fmt.Sprintf("SECRET-%s-L%d", rel, h.Line),
			Severity: h.Severity,
			Category: "secret",
			Title:    h.Message,
			Source:   verdict.SourceSecret,
			Evidence: []verdict.Evidence{{File: rel, Line: h.Line, Snippet: h.Snippet}},
		})
	}
	return out
}

// runNoLLMScan implements `assay scan --no-llm`: the deterministic tier
// (secret sweep + SCA/CVE + poison floor), no Claude client, no API key. The
// caller (newScanCmd's RunE) MUST invoke this before any client construction
// — that ordering is the property under test.
func runNoLLMScan(cmd *cobra.Command, targetPath string, targetItem *inventory.Item, offline bool, format, output string, asJSON bool, failOn string, pol *policy.Policy, paths *store.Paths) error {
	ctx := cmd.Context()

	// 1. Deterministic secret sweep (real file:line evidence).
	hits, err := prepass.ScanSecrets(targetPath, prepass.Options{Offline: offline})
	if err != nil {
		return fmt.Errorf("no-llm secret scan: %w", err)
	}
	findings := secretsToFindings(hits, targetPath)

	// 2. Deterministic floor: SCA (OSV, skipped offline) + poison (always).
	findings = floor.Apply(ctx, targetPath, offline, findings)

	// 3. Policy suppressions before the verdict is computed.
	if pol != nil {
		var suppressed []verdict.Finding
		findings, suppressed = pol.Apply(findings, time.Now())
		for _, s := range suppressed {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Suppressed [%s] %s per policy\n", s.Severity, s.ID)
		}
	}
	if findings == nil {
		findings = []verdict.Finding{}
	}

	// 4. Assemble the schema verdict — no LLM prose fields.
	target := verdict.Target{Kind: "claude-code-plugin", Name: filepath.Base(targetPath), Source: "local://" + targetPath}
	if targetItem != nil {
		target.Kind = string(targetItem.Kind)
		target.Name = targetItem.Name
		if targetItem.Version != "" {
			target.Version = targetItem.Version
		}
		if targetItem.Source != "" {
			target.Source = targetItem.Source
		}
		if targetItem.Hash != "" {
			target.Hash = targetItem.Hash
		}
	}
	label := recomputeVerdictFromValidated(findings)
	v := verdict.Verdict{
		SchemaVersion: verdict.SchemaVersion,
		Target:        target,
		ScannedAt:     time.Now().UTC(),
		Scanner:       verdict.Scanner{Name: verdict.ScannerName, Version: scannerVersion, Model: "none", PromptVersion: verdict.PromptVersionNoLLM},
		Verdict:       label,
		Summary:       fmt.Sprintf("Deterministic scan (no LLM): %d finding(s) from secrets + SCA + poison.", len(findings)),
		Findings:      findings,
	}

	// 5. Allocate scan dir + write artifacts (+ SARIF, + card.md).
	history := store.NewHistory(paths.ScansDir)
	scanDir, scanID, err := history.Allocate(filepath.Base(targetPath))
	if err != nil {
		return fmt.Errorf("allocate scan dir: %w", err)
	}
	v.ScanID = scanID
	card := verdict.RenderCard(v, verdict.CardOptions{})
	if err := verdict.Write(scanDir, v, verdict.RenderMarkdown(v), "no-llm deterministic scan\n"); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(scanDir, "card.md"), []byte(card), 0o600); err != nil {
		return fmt.Errorf("write card.md: %w", err)
	}
	if format == "sarif" {
		sarifBytes, serr := verdict.ToSARIF(v)
		if serr != nil {
			return fmt.Errorf("serialize SARIF: %w", serr)
		}
		if werr := os.WriteFile(filepath.Join(scanDir, "audit.sarif"), sarifBytes, 0o600); werr != nil {
			return fmt.Errorf("write SARIF: %w", werr)
		}
	}
	if strings.TrimSpace(output) != "" {
		if err := copyArtifactsTo(scanDir, output); err != nil {
			return fmt.Errorf("copy artifacts to --output: %w", err)
		}
	}

	// 6. Emit the card + optional JSON.
	_, _ = fmt.Fprint(cmd.OutOrStdout(), card)
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Artifacts: %s\n", scanDir)
	if asJSON {
		out, _ := json.MarshalIndent(v, "", "  ")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), string(out))
	}

	// 7. Policy deny-categories, then the CI gate (same semantics as full scan).
	if hits := pol.DeniedCategoryHits(v.Findings); len(hits) > 0 {
		msg := fmt.Sprintf("assay: %d finding(s) in policy deny_categories (exit 2)", len(hits))
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), msg)
		return &exitCodeError{code: 2, msg: msg}
	}
	// Precedence: explicit --fail-on flag > policy.fail_on > default (unsafe).
	// Mirrors the full-scan gate in cmd_scan.go exactly.
	effectiveFailOn := failOn
	if !cmd.Flags().Changed("fail-on") && pol != nil && pol.FailOn != "" {
		effectiveFailOn = pol.FailOn
	}
	if gate := evalFailOn(v.Verdict, len(v.Findings), effectiveFailOn); gate != nil {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), gate.msg)
		return gate
	}
	return nil
}
