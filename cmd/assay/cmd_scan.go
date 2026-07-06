package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/chawdamrunal/assay/internal/auth"
	"github.com/chawdamrunal/assay/internal/claude"
	"github.com/chawdamrunal/assay/internal/floor"
	"github.com/chawdamrunal/assay/internal/inventory"
	"github.com/chawdamrunal/assay/internal/policy"
	"github.com/chawdamrunal/assay/internal/scanner"
	"github.com/chawdamrunal/assay/internal/server"
	"github.com/chawdamrunal/assay/internal/store"
	"github.com/chawdamrunal/assay/internal/verdict"
)

// newDetachedCommand constructs an exec.Cmd whose stdio is /dev/null. Used
// by --spawn-deep so the deep scan continues after the parent exits.
func newDetachedCommand(name string, args ...string) *exec.Cmd {
	c := exec.Command(name, args...) // #nosec G204 -- name is our own binary path
	c.Stdin = nil
	c.Stdout = nil
	c.Stderr = nil
	return c
}

// scannerVersion is what we write into verdict.Scanner.Version. Linker can override via -ldflags.
var scannerVersion = "0.1.0-dev"

// runQuickScan implements `assay scan --quick`. It runs the deterministic
// pre-pass + risk heuristic (scanner.RunQuick) and either:
//   - prints a tabular summary to stdout, OR
//   - emits a one-line JSON payload (when --json) suitable for the hook
//     script that gates `/plugin install`.
//
// When spawnDeep is true, additionally forks `assay scan <target>` (full
// MCP-mode scan) in the background and records the deep_scan_id in the JSON
// output so the hook can deep-link to it later.
func runQuickScan(cmd *cobra.Command, target string, asJSON, spawnDeep bool, paths *store.Paths) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), 25*time.Second)
	defer cancel()
	result, err := scanner.RunQuick(ctx, target)
	if err != nil {
		return fmt.Errorf("quick scan: %w", err)
	}

	if spawnDeep {
		deepID, err := spawnDeepScan(target, paths)
		if err == nil {
			result.DeepScanID = deepID
		}
		// Spawn errors are non-fatal — gate decisions should never block on
		// the background deep scan succeeding. The triage verdict stands.
	}

	if asJSON {
		out, err := result.MarshalCompact()
		if err != nil {
			return err
		}
		_, _ = cmd.OutOrStdout().Write(out)
		_, _ = cmd.OutOrStdout().Write([]byte("\n"))
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Quick scan of %s\n", target)
	fmt.Fprintf(cmd.OutOrStdout(), "  Risk: %s\n", result.Risk)
	fmt.Fprintf(cmd.OutOrStdout(), "  Summary: %s\n", result.SummaryLine())
	if result.DeepScanID != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  Deep scan (background): %s\n", result.DeepScanID)
	}
	return nil
}

// spawnDeepScan forks an `assay scan <target>` subprocess that runs full
// MCP-mode (no --quick) and detaches so the parent process can return
// immediately. Stdin/stdout/stderr go to /dev/null; the deep scan's results
// land in ~/.assay/scans/<target>/<scan_id>/ as usual.
func spawnDeepScan(target string, paths *store.Paths) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		exe = "assay"
	}
	// Allocate scan_id up-front so we can return it before the subprocess
	// exits. The subprocess will resolve via assay serve's StartScan path —
	// but we're outside serve, so use the CLI scan command which produces a
	// timestamp ID. To make the ID predictable for the hook to surface in
	// "deep scan started" messaging, we delegate to the CLI's own allocator
	// and pass --output to a fresh subdir whose name IS the deep_scan_id.
	id := time.Now().UTC().Format("20060102T150405.000Z")
	outDir := filepath.Join(paths.ScansDir, filepath.Base(target), id)
	cmd := os.Args[0:0] // placeholder; we use exec.Command directly below
	_ = cmd

	// Build the subprocess. exec.Command will inherit minimal env.
	c := newDetachedCommand(exe, "scan", target, "--output", outDir)
	if err := c.Start(); err != nil {
		return "", err
	}
	// Don't wait — the deep scan runs detached.
	_ = c.Process.Release()
	return id, nil
}

// scanClientFactory is a test seam — when non-nil, returns a Client (e.g., FakeClient)
// instead of constructing a real Anthropic client. Tests set this to a FakeClient.
var scanClientFactory func(ctx context.Context) (claude.Client, error)

func newScanCmd() *cobra.Command {
	var (
		flagBudget      float64
		flagModel       string
		flagModelInv    string
		flagOutput      string
		flagOffline     bool
		flagConcurrency int
		flagNoCache     bool
		flagJSON        bool
		flagClaudeDir   string
		flagQuick       bool
		flagSpawnDeep   bool
		flagFailOn      string
		flagFormat      string
		flagPolicy      string
		flagNoLLM       bool
	)

	cmd := &cobra.Command{
		Use:   "scan <target>",
		Short: "Run a full security scan against a Claude Code plugin, MCP server, or local directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			paths, err := resolvePaths()
			if err != nil {
				return err
			}
			if err := paths.Ensure(); err != nil {
				return err
			}

			cfg, err := store.LoadConfig(paths.ConfigFile)
			if err != nil {
				return err
			}

			switch flagFormat {
			case "", "text", "sarif":
			default:
				return fmt.Errorf("invalid --format %q (want: text | sarif)", flagFormat)
			}

			// Resolve target: a path (absolute or relative to cwd) or an inventory name.
			targetPath, targetItem, err := resolveScanTarget(args[0], paths, flagClaudeDir)
			if err != nil {
				return fmt.Errorf("resolve target: %w", err)
			}

			// Policy-as-code. Loaded from the SCANNING USER's side (--policy or
			// the cwd), never from the target tree — otherwise a malicious
			// plugin could ship a policy to suppress its own findings.
			cwd, _ := os.Getwd()
			var pol *policy.Policy
			if pp := policy.Resolve(flagPolicy, cwd); pp != "" {
				pol, err = policy.Load(pp)
				if err != nil {
					return fmt.Errorf("policy: %w", err)
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Policy: %s\n", pp)
			}
			if pol.IsAllowlisted(targetPath) {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Target %s is allowlisted by policy — skipping scan.\n", filepath.Base(targetPath))
				return nil
			}

			// --quick: tier-1 deterministic profile (no LLM call). Forks a deep
			// scan in the background if --spawn-deep is also set; otherwise the
			// caller (e.g. the hook script) chooses whether to escalate.
			if flagQuick {
				return runQuickScan(cmd, targetPath, flagJSON, flagSpawnDeep, paths)
			}

			// --no-llm: deterministic tier — secrets + SCA + poison, no LLM,
			// no credentials. The GitHub Action's zero-secret path uses this.
			if flagNoLLM {
				return runNoLLMScan(cmd, targetPath, targetItem, flagOffline, flagFormat, flagOutput, flagJSON, flagFailOn, pol, paths)
			}

			// Determine effective options.
			model := flagModel
			if model == "" {
				model = cfg.Models.Default
			}
			modelInv := flagModelInv
			if modelInv == "" {
				modelInv = cfg.Models.Investigation
			}
			concurrency := flagConcurrency
			if concurrency == 0 {
				concurrency = cfg.Scan.SubagentConcurrency
			}
			budget := flagBudget
			if budget == 0 {
				budget = cfg.Scan.BudgetUSD
			}

			// Build the Claude client (real or fake).
			client, err := newScanClient(cmd.Context(), paths)
			if err != nil {
				return err
			}
			// Decorate: retry on 429/5xx (the dominant subscription-bearer
			// failure mode), then track budget.
			client = claude.NewRetryingClient(client, claude.RetryConfig{
				Notify: func(attempt int, wait time.Duration, _ error) {
					fmt.Fprintf(cmd.ErrOrStderr(),
						"  rate-limited (attempt %d) — retrying in %s\n",
						attempt, wait.Round(100*time.Millisecond))
				},
			})
			budgetTracker := claude.NewBudget(budget)
			client = claude.NewBudgetClient(client, budgetTracker)

			// Allocate scan directory.
			history := store.NewHistory(paths.ScansDir)
			scanDir, scanID, err := history.Allocate(filepath.Base(targetPath))
			if err != nil {
				return fmt.Errorf("allocate scan dir: %w", err)
			}

			// Print preamble.
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Scanning %s\n", targetPath)
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Scan ID: %s\n", scanID)
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Output: %s\n\n", scanDir)

			// Run the scan. Events stream to stdout as terse progress lines.
			events := make(chan scanner.Event, 64)
			done := make(chan struct{})
			go func() {
				for e := range events {
					switch {
					case e.Status == "start":
						_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  -> %s ...\n", e.Stage)
					case e.Status == "complete" && e.Message != "":
						_, _ = fmt.Fprintf(cmd.OutOrStdout(), "    %s\n", e.Message)
					case e.Status == "error":
						_, _ = fmt.Fprintf(cmd.OutOrStderr(), "    !! %s: %s\n", e.Stage, e.Message)
					}
				}
				close(done)
			}()

			result, err := scanner.Scan(cmd.Context(), scanner.Options{
				Target:              targetPath,
				ClaudeDir:           flagClaudeDir,
				Model:               model,
				ModelInvestigation:  modelInv,
				BudgetUSD:           budget,
				SubagentConcurrency: concurrency,
				Offline:             flagOffline,
			}, client, events)
			close(events)
			<-done
			if err != nil {
				return fmt.Errorf("scan: %w", err)
			}

			// Validate citations.
			scannerFindings := result.Findings
			validatedFindings, dropped := verdict.Validate(targetPath, convertToVerdictFindings(scannerFindings))
			if len(dropped) > 0 {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "\nDropped %d unverifiable finding(s) (snippet not found at cited location):\n", len(dropped))
				for _, d := range dropped {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  - [%s] %s - %s\n", d.Severity, d.Title, d.Reason)
				}
			}

			// Deterministic floor: append SCA + poison findings so the
			// legacy/CLI path produces the same audit as MCP mode (previously
			// only the MCP finalize path applied the floor — a silent trust
			// gap for --scan-mode legacy / CLI users). Offline skips the OSV
			// network lookup.
			validatedFindings = floor.Apply(cmd.Context(), targetPath, flagOffline, validatedFindings)

			// Apply policy suppressions (reviewed/accepted findings) before the
			// verdict is computed, so suppressed findings don't drive it. Print
			// the reasons so suppression stays visible/auditable.
			if pol != nil {
				var suppressed []verdict.Finding
				validatedFindings, suppressed = pol.Apply(validatedFindings, time.Now())
				if len(suppressed) > 0 {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "\nSuppressed %d finding(s) per policy:\n", len(suppressed))
					for _, s := range suppressed {
						_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  - [%s] %s\n", s.Severity, s.ID)
					}
				}
			}

			// Build the public-schema verdict.
			target := verdict.Target{
				Kind:   "claude-code-plugin",
				Name:   filepath.Base(targetPath),
				Source: "local://" + targetPath,
			}
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

			publicVerdict := verdict.FromScanner(result, target, scannerVersion)
			// Replace the FromScanner-converted findings with the validated subset.
			publicVerdict.Findings = validatedFindings
			if publicVerdict.Findings == nil {
				publicVerdict.Findings = []verdict.Finding{}
			}

			// Recompute verdict label based on validated findings (some may have been dropped).
			publicVerdict.Verdict = recomputeVerdictFromValidated(validatedFindings)

			// Shared verdict card — identical rendering to --no-llm and the
			// GitHub Action's PR comment, giving CLI/PR parity.
			card := verdict.RenderCard(publicVerdict, verdict.CardOptions{DroppedCount: len(dropped)})

			// Write artifacts.
			audit := result.AuditMarkdown
			if strings.TrimSpace(audit) == "" {
				audit = verdict.RenderMarkdown(publicVerdict)
			}
			investigationLog := fmt.Sprintf("Scan %s of %s\nModel: %s\nPrompt version: %s\nBudget cap: $%.2f\nBudget spent: $%.4f\n",
				scanID, targetPath, model, result.PromptVersion, budget, budgetTracker.SpentUSD())
			if err := verdict.Write(scanDir, publicVerdict, audit, investigationLog); err != nil {
				return err
			}
			if werr := os.WriteFile(filepath.Join(scanDir, "card.md"), []byte(card), 0o600); werr != nil {
				return fmt.Errorf("write card.md: %w", werr)
			}

			// SARIF output for CI / code-scanning consumers (GitHub Advanced
			// Security, GitLab SAST). Written next to audit.json so --output
			// copies it too; the GitHub Action uploads audit.sarif.
			if flagFormat == "sarif" {
				sarifBytes, serr := verdict.ToSARIF(publicVerdict)
				if serr != nil {
					return fmt.Errorf("serialize SARIF: %w", serr)
				}
				if werr := os.WriteFile(filepath.Join(scanDir, "audit.sarif"), sarifBytes, 0o600); werr != nil {
					return fmt.Errorf("write SARIF: %w", werr)
				}
			}

			// Print the shared verdict card (CLI/PR parity with --no-llm and the Action).
			_, _ = fmt.Fprint(cmd.OutOrStdout(), "\n"+card)
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "\nBudget spent: $%.4f / $%.2f\n", budgetTracker.SpentUSD(), budget)
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Artifacts: %s\n", scanDir)
			if flagFormat == "sarif" {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "SARIF: %s\n", filepath.Join(scanDir, "audit.sarif"))
			}

			if flagJSON {
				out, _ := json.MarshalIndent(publicVerdict, "", "  ")
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), string(out))
			}

			// --output: copy audit.json + audit.md to the requested directory
			// in addition to the canonical scan history. The history copy is
			// authoritative; this is a convenience for users piping artifacts
			// into a CI workspace or shared report dir.
			if strings.TrimSpace(flagOutput) != "" {
				if err := copyArtifactsTo(scanDir, flagOutput); err != nil {
					return fmt.Errorf("copy artifacts to --output: %w", err)
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Also written to: %s\n", flagOutput)
			}

			// Policy deny-categories: fail if any surviving finding is in a
			// denied category, regardless of severity threshold.
			if hits := pol.DeniedCategoryHits(publicVerdict.Findings); len(hits) > 0 {
				cats := map[string]bool{}
				for _, h := range hits {
					cats[h.Category] = true
				}
				list := make([]string, 0, len(cats))
				for c := range cats {
					list = append(list, c)
				}
				msg := fmt.Sprintf("assay: %d finding(s) in policy deny_categories %v (exit 2)", len(hits), list)
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), msg)
				return &exitCodeError{code: 2, msg: msg}
			}

			// CI gate: exit non-zero when the verdict meets the threshold.
			// Precedence: explicit --fail-on flag > policy.fail_on > default
			// (unsafe). Exit code 2 (distinct from 1 = crash) is honored by main().
			effectiveFailOn := flagFailOn
			if !cmd.Flags().Changed("fail-on") && pol != nil && pol.FailOn != "" {
				effectiveFailOn = pol.FailOn
			}
			if gate := evalFailOn(publicVerdict.Verdict, len(publicVerdict.Findings), effectiveFailOn); gate != nil {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), gate.msg)
				return gate
			}
			return nil
		},
	}

	cmd.Flags().Float64Var(&flagBudget, "budget", 0, "USD cap for this scan (default from config)")
	cmd.Flags().StringVar(&flagModel, "model", "", "Anthropic model for stages 0-2,4-5 (default from config)")
	cmd.Flags().StringVar(&flagModelInv, "model-investigation", "", "Model for Stage 3 investigators (default from config)")
	cmd.Flags().StringVar(&flagOutput, "output", "", "Write artifacts to this directory (default ~/.assay/scans/)")
	cmd.Flags().BoolVar(&flagOffline, "offline", false, "Skip OSV CVE lookups")
	cmd.Flags().IntVar(&flagConcurrency, "concurrency", 0, "Stage 3 sub-agent concurrency (default from config)")
	cmd.Flags().BoolVar(&flagNoCache, "no-cache", false, "Bypass scan cache")
	// --no-cache is reserved for v0.6 when the SBOM/attestation cache lands.
	// Hide it until then so users don't expect behaviour we can't deliver.
	_ = cmd.Flags().MarkHidden("no-cache")
	cmd.Flags().BoolVar(&flagJSON, "json", false, "Print verdict JSON to stdout in addition to writing files")
	cmd.Flags().StringVar(&flagClaudeDir, "claude-dir", "", "Path to ~/.claude (for inventory lookups)")
	cmd.Flags().BoolVar(&flagQuick, "quick", false, "Tier-1 triage-only scan (no LLM call). Returns risk={low|medium|high|critical} in <2s. Pair with --json for the pre-install gate hook.")
	cmd.Flags().BoolVar(&flagNoLLM, "no-llm", false, "Deterministic scan only (secrets + SCA/CVE + poison), no LLM call, no API key required.")
	cmd.Flags().BoolVar(&flagSpawnDeep, "spawn-deep", false, "When used with --quick, forks a detached full MCP-mode scan in the background and reports its scan_id.")
	cmd.Flags().StringVar(&flagFailOn, "fail-on", "unsafe", "Exit code 2 when the verdict meets this threshold: unsafe | caution | any | off")
	cmd.Flags().StringVar(&flagFormat, "format", "text", "Output format: text | sarif (sarif writes audit.sarif for CI code-scanning upload)")
	cmd.Flags().StringVar(&flagPolicy, "policy", "", "Path to .assay-policy.json (suppressions, deny-categories, allowlist). Default: ./.assay-policy.json if present")
	return cmd
}

// evalFailOn returns a non-nil *exitCodeError (code 2) when the verdict meets
// the requested CI gate threshold. Thresholds:
//   - "off"/"never"/"none" → never gate
//   - "any"                → gate if any finding survived validation
//   - "caution"            → gate on caution OR unsafe
//   - "unsafe" (default)   → gate on unsafe only
func evalFailOn(verdictLabel string, findingCount int, failOn string) *exitCodeError {
	trigger := false
	switch strings.ToLower(strings.TrimSpace(failOn)) {
	case "off", "never", "none":
		trigger = false
	case "any":
		trigger = findingCount > 0
	case "caution":
		trigger = verdictLabel == "caution" || verdictLabel == "unsafe"
	default: // "", "unsafe", or anything unrecognized → default to unsafe
		trigger = verdictLabel == "unsafe"
	}
	if !trigger {
		return nil
	}
	return &exitCodeError{
		code: 2,
		msg:  fmt.Sprintf("assay: verdict %q meets --fail-on=%s threshold (exit 2)", verdictLabel, failOn),
	}
}

// copyArtifactsTo copies audit.json + audit.md (plus the verdict JSON if
// present) from a canonical scan directory into a user-supplied output
// directory. Used by the --output flag so users can route scan artifacts
// into a CI workspace or shared report dir without giving up the canonical
// history copy.
func copyArtifactsTo(scanDir, outDir string) error {
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		return err
	}
	for _, name := range []string{"audit.json", "audit.md", "audit.sarif", "card.md"} {
		src := filepath.Join(scanDir, name)
		data, err := os.ReadFile(src) // #nosec G304 -- scanDir under our own ScansDir
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return fmt.Errorf("read %s: %w", name, err)
		}
		if err := os.WriteFile(filepath.Join(outDir, name), data, 0o600); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
	}
	return nil
}

// newScanClient returns a Client: the test seam factory if set, else a real client.
func newScanClient(ctx context.Context, paths *store.Paths) (claude.Client, error) {
	if scanClientFactory != nil {
		return scanClientFactory(ctx)
	}
	_ = paths
	creds, err := auth.Resolve(configKeyringService)
	if err != nil {
		return nil, fmt.Errorf("anthropic credentials: %w", err)
	}
	return claude.NewRealClientFromCredentials(creds, nil)
}

// resolveScanTarget interprets target as either a filesystem path or an inventory name.
func resolveScanTarget(target string, _ *store.Paths, claudeDir string) (string, *inventory.Item, error) {
	// Direct path?
	if info, err := os.Stat(target); err == nil && info.IsDir() {
		abs, _ := filepath.Abs(target)
		return abs, nil, nil
	}

	// Look up in inventory. ResolveClaudeDir picks the OS-appropriate
	// default (~/.claude on macOS/Linux, %APPDATA%\Claude on Windows
	// with ~/.claude as fallback).
	if claudeDir == "" {
		claudeDir = server.ResolveClaudeDir()
	}
	inv, err := inventory.EnumerateAll(inventory.OptionsForClaudeDir(claudeDir))
	if err == nil {
		for _, it := range inv.Items {
			if it.Name == target && it.LocalPath != "" {
				item := it
				return it.LocalPath, &item, nil
			}
		}
	}

	return "", nil, errors.New("target not found as path or inventory name: " + target)
}

func convertToVerdictFindings(in []scanner.FindingOut) []verdict.Finding {
	out := make([]verdict.Finding, 0, len(in))
	for _, f := range in {
		fout := verdict.Finding{
			ID: f.ID, Severity: f.Severity, Category: f.Category, Title: f.Title,
			Description: f.Description, ExploitScenario: f.ExploitScenario, ThreatID: f.ThreatID,
		}
		for _, e := range f.Evidence {
			fout.Evidence = append(fout.Evidence, verdict.Evidence{File: e.File, Line: e.Line, Snippet: e.Snippet})
		}
		out = append(out, fout)
	}
	return out
}

func recomputeVerdictFromValidated(findings []verdict.Finding) string {
	var critical, high, medium, lowInfo int
	for _, f := range findings {
		switch f.Severity {
		case "critical":
			critical++
		case "high":
			high++
		case "medium":
			medium++
		case "low", "info":
			lowInfo++
		}
	}
	switch {
	case critical > 0 || high > 0:
		return "unsafe"
	case medium > 0:
		return "caution"
	case lowInfo >= 3:
		return "caution"
	default:
		return "safe"
	}
}
