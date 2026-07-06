package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/chawdamrunal/assay/internal/api"
	"github.com/chawdamrunal/assay/internal/claude"
	"github.com/chawdamrunal/assay/internal/floor"
	assaymcp "github.com/chawdamrunal/assay/internal/mcp"
	"github.com/chawdamrunal/assay/internal/policy"
	"github.com/chawdamrunal/assay/internal/provider"
	"github.com/chawdamrunal/assay/internal/scanner"
	"github.com/chawdamrunal/assay/internal/server"
	"github.com/chawdamrunal/assay/internal/store"
	"github.com/chawdamrunal/assay/internal/verdict"
)

func newServeCmd() *cobra.Command {
	var bind string
	var claudeDir string
	var fake bool
	var fakeFixturesDir string
	var scanMode string
	var claudeBin string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Launch the local Assay web UI",
		RunE: func(cmd *cobra.Command, _ []string) error {
			paths, err := resolvePaths()
			if err != nil {
				return err
			}
			if err := paths.Ensure(); err != nil {
				return err
			}
			// --fake is shorthand for --scan-mode fake (preserved for back-compat).
			if fake {
				scanMode = "fake"
			}
			if scanMode == "" {
				scanMode = "mcp"
			}
			if scanMode == "mcp" {
				if err := assaymcp.CheckClaudeAvailable(claudeBin); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(),
						"warn: Claude Code CLI not available (%v) — falling back to legacy orchestrator. Install via https://claude.com/code or rerun with --scan-mode legacy to silence this.\n",
						err)
					scanMode = "legacy"
				}
			}

			ready := make(chan string, 1)
			stop := make(chan struct{})

			// Bridge SIGINT/SIGTERM to stop.
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sigCh
				close(stop)
			}()

			// Print friendly URL once the listener is up.
			finalMode := scanMode
			go func() {
				if addr, ok := <-ready; ok {
					banner := fmt.Sprintf("Assay serving at http://%s\n", addr)
					banner += fmt.Sprintf("Scan mode: %s\n", finalMode)
					switch finalMode {
					case "fake":
						banner += "  scans replay testdata/recorded/<basename>.json — no LLM call\n"
					case "mcp":
						banner += "  scans spawn `claude -p` and drive the assay MCP server — uses your Claude Code subscription quota\n"
					case "legacy":
						banner += "  scans use the in-process Go orchestrator — needs ANTHROPIC_API_KEY or `assay auth` configured\n"
					}
					_, _ = fmt.Fprint(cmd.OutOrStdout(), banner)
				}
			}()

			return runServe(bind, api.NewFrontendHandler(), ready, stop, paths, claudeDir, scanMode, fakeFixturesDir, claudeBin)
		},
	}

	cmd.Flags().StringVar(&bind, "bind", "127.0.0.1:7373", "Address to bind (default localhost only)")
	cmd.Flags().StringVar(&claudeDir, "claude-dir", "", "Path to ~/.claude (overrides default)")
	cmd.Flags().StringVar(&scanMode, "scan-mode", "", "How to run scans: mcp (default, uses Claude Code) | legacy (in-process Go orchestrator) | fake (replay fixtures)")
	cmd.Flags().StringVar(&claudeBin, "claude-bin", "claude", "Path to the Claude Code CLI (used by --scan-mode mcp)")
	cmd.Flags().BoolVar(&fake, "fake", false, "Shorthand for --scan-mode fake")
	cmd.Flags().StringVar(&fakeFixturesDir, "fake-fixtures-dir", "", "Override the directory holding fixture JSONs (default: repo testdata/recorded)")
	return cmd
}

// runServe starts the HTTP server. ready receives the bound address once the
// listener is up. stop closes to request shutdown. Returns nil on graceful
// stop or an error on listener / serve failure.
func runServe(bind string, frontend http.Handler, ready chan<- string, stop <-chan struct{}, paths *store.Paths, claudeDir, scanMode, fakeFixturesDir, claudeBin string) error {
	rt := server.NewRuntime(paths, claudeDir)
	runner := api.NewScanRunner()

	// Server-lifetime context: canceled on shutdown so background scan/fleet
	// goroutines stop cleanly instead of being orphaned for the supervisor to
	// SIGKILL mid-write.
	srvCtx, srvCancel := context.WithCancel(context.Background())
	defer srvCancel()

	// Policy-as-code, resolved ONCE at boot from the serving user's cwd (never
	// the target tree). Applied to every scan's findings so suppressions /
	// deny-categories / allowlist work through the web UI, not just the CLI.
	var pol *policy.Policy
	cwd, _ := os.Getwd()
	if pp := policy.Resolve("", cwd); pp != "" {
		loaded, perr := policy.Load(pp)
		if perr != nil {
			return fmt.Errorf("policy %s: %w", pp, perr)
		}
		pol = loaded
		log.Printf("serve: policy loaded from %s", pp)
	}

	reg := provider.NewRegistry(configKeyringService)
	var startScan api.StartScanFunc
	switch scanMode {
	case "fake":
		startScan = buildFakeStartScan(paths, fakeFixturesDir, pol)
	case "legacy":
		startScan = buildStartScan(paths, pol, reg)
	case "mcp", "":
		startScan = buildRoutedStartScan(paths, claudeBin, pol, reg)
	default:
		return fmt.Errorf("unknown scan-mode %q", scanMode)
	}
	// MarketplacesDir feeds the chat assistant's resolver — it's the
	// fall-back lookup for plugin names that aren't installed yet but live
	// in the user's marketplace cache. ResolveClaudeDir picks the OS-
	// appropriate default (Windows: %APPDATA%\Claude with ~/.claude as
	// fallback; macOS/Linux: ~/.claude).
	resolvedClaudeDir := claudeDir
	if resolvedClaudeDir == "" {
		resolvedClaudeDir = server.ResolveClaudeDir()
	}
	marketplacesDir := ""
	if resolvedClaudeDir != "" {
		marketplacesDir = filepath.Join(resolvedClaudeDir, "plugins", "marketplaces")
	}

	// GitHub clone cache — every on-demand fetch lands here so the
	// path-guard knows which dir to trust and so multiple chat-initiated
	// scans share the same cached checkout.
	githubCacheDir := filepath.Join(paths.DataDir, "sources")
	if err := os.MkdirAll(githubCacheDir, 0o750); err != nil {
		return fmt.Errorf("mkdir github cache: %w", err)
	}

	// Extend the path-guard allowed roots with the GitHub cache so the
	// auto-clone flow's resulting path passes EnsureAllowed.
	allowedRoots := append(api.DefaultAllowedRoots(resolvedClaudeDir), githubCacheDir)

	handler := api.NewHandler(api.Deps{
		LoadInventory:   rt.LoadInventory,
		ConfigPath:      rt.ConfigPath,
		ScansDir:        rt.ScansDir,
		FleetDir:        filepath.Join(paths.DataDir, "fleet"),
		MarketplacesDir: marketplacesDir,
		GitHubCacheDir:  githubCacheDir,
		KeychainService: configKeyringService,
		Status: api.StatusDeps{
			ClaudeBin:       claudeBin,
			ScansDir:        rt.ScansDir,
			HookScript:      filepath.Join(paths.DataDir, "hooks", "assay-pre-install.sh"),
			KeychainService: configKeyringService,
		},
		Scans: api.ScansDeps{
			ScansDir:     rt.ScansDir,
			Runner:       runner,
			StartScan:    startScan,
			AllowedRoots: allowedRoots,
		},
		ServerCtx: srvCtx,
		Frontend:  frontend,
	})

	ln, err := net.Listen("tcp", bind)
	if err != nil {
		return fmt.Errorf("listen %s: %w", bind, err)
	}
	// Signal the bound address (handles :0 by reporting the picked port).
	ready <- ln.Addr().String()
	close(ready)

	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		err := srv.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	select {
	case <-stop:
		// Cancel in-flight scan/fleet goroutines first, then drain HTTP.
		srvCancel()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		return <-serveErr
	case err := <-serveErr:
		return err
	}
}

// buildStartScan returns the StartScan closure used by the HTTP API. The closure
// runs an entire scan in the background: resolve auth, run the 5-stage scanner,
// validate citations, write artifacts, and bridge scanner events to the runner
// so SSE subscribers see live progress. The runner channel is always closed
// (via runner.Complete) so subscribers never hang.
func buildStartScan(paths *store.Paths, pol *policy.Policy, reg *provider.Registry) api.StartScanFunc {
	return func(ctx context.Context, scanID, target string, offline bool, since string, runner *api.ScanRunner) {
		_ = since // legacy in-process orchestrator does not yet honor auto-diff

		// scanDir is set as soon as we successfully allocate it; failures
		// after that point write error.json into the dir so the SPA can
		// render a real "scan failed" page instead of 404.
		var scanDir string
		// Helpers: emit a stage error, persist it if we have a scanDir, then close.
		fail := func(stage, msg string) {
			runner.Emit(scanID, scanner.Event{Stage: stage, Status: "error", Message: msg})
			if scanDir != "" {
				writeScanError(scanDir, scanID, target, stage, msg)
			}
			runner.Complete(scanID)
		}

		cfg, err := store.LoadConfig(paths.ConfigFile)
		if err != nil {
			log.Printf("serve: scan %s load config failed: %v", scanID, err)
			fail("config", err.Error())
			return
		}

		// Resolve the direct-API provider from config; this in-process path
		// defaults to anthropic-api when config names a CLI agent (the router
		// sends CLI agents to the MCP-spawn path instead, so they don't reach
		// here). The registry builds the base client (Anthropic now; Gemini /
		// OpenAI as they land).
		provID := provider.AgentID(cfg.Models.Provider).Resolve()
		if !provID.IsAPI() {
			provID = provider.AgentAnthropicAPI
		}
		baseClient, err := reg.NewClient(provID)
		if err != nil {
			log.Printf("serve: scan %s client init failed: %v", scanID, err)
			fail("auth", err.Error())
			return
		}
		budgetTracker := claude.NewBudget(cfg.Scan.BudgetUSD)
		// Wrap the base client with retry-on-429/5xx FIRST, then budget tracking.
		// Order matters: retries inside the budget envelope are still counted,
		// but a transient 429 no longer kills the scan.
		retrying := claude.NewRetryingClient(baseClient, claude.RetryConfig{
			Notify: func(attempt int, wait time.Duration, _ error) {
				runner.Emit(scanID, scanner.Event{
					Stage:   "scan",
					Status:  "start",
					Message: fmt.Sprintf("rate-limited (attempt %d) — retrying in %s", attempt, wait.Round(100*time.Millisecond)),
				})
			},
		})
		client := claude.NewBudgetClient(retrying, budgetTracker)

		// Allocate scan dir with the API-supplied scanID so artifacts live where
		// GET /api/scans/:id looks for them. DeriveTargetName (not filepath.Base)
		// so a marketplace plugin installed under .../cache/<m>/<name>/<version>/
		// buckets under <name>, not its version dir.
		history := store.NewHistory(paths.ScansDir)
		targetName := assaymcp.DeriveTargetName(target)
		allocatedDir, err := history.AllocateAt(targetName, scanID)
		if err != nil {
			log.Printf("serve: scan %s allocate dir failed: %v", scanID, err)
			fail("prepass", err.Error())
			return
		}
		scanDir = allocatedDir

		// Bridge scanner events to the runner.
		evCh := make(chan scanner.Event, 64)
		bridgeDone := make(chan struct{})
		go func() {
			for e := range evCh {
				runner.Emit(scanID, e)
			}
			close(bridgeDone)
		}()

		result, scanErr := scanner.Scan(ctx, scanner.Options{
			Target:              target,
			Model:               cfg.Models.Default,
			ModelInvestigation:  cfg.Models.Investigation,
			BudgetUSD:           cfg.Scan.BudgetUSD,
			SubagentConcurrency: cfg.Scan.SubagentConcurrency,
			Offline:             offline,
		}, client, evCh)
		close(evCh)
		<-bridgeDone

		if scanErr != nil {
			log.Printf("serve: scan %s failed: %v", scanID, scanErr)
			runner.Emit(scanID, scanner.Event{Stage: "done", Status: "error", Message: scanErr.Error()})
			writeScanError(scanDir, scanID, target, "scan", scanErr.Error())
			runner.Complete(scanID)
			return
		}

		// Override the orchestrator's auto-generated UUID with our API-allocated scanID.
		result.ScanID = scanID

		// Validate citations, then run the deterministic floor (SCA/poison) +
		// policy suppression — same as the CLI and MCP paths, so legacy serve
		// scans no longer silently omit CVE/poison findings or ignore policy.
		validatedFindings, _ := verdict.Validate(target, convertToVerdictFindings(result.Findings))
		validatedFindings, denied := applyFloorAndPolicy(ctx, target, offline, pol, validatedFindings)

		publicVerdict := verdict.FromScanner(result, verdict.Target{
			Kind:   "claude-code-plugin",
			Name:   targetName,
			Source: "local://" + target,
		}, scannerVersion)
		publicVerdict.Findings = validatedFindings
		if publicVerdict.Findings == nil {
			publicVerdict.Findings = []verdict.Finding{}
		}
		publicVerdict.Verdict = recomputeVerdictFromValidated(validatedFindings)
		if denied {
			publicVerdict.Verdict = "unsafe"
			publicVerdict.Summary = policyDenyNote + publicVerdict.Summary
		}

		audit := result.AuditMarkdown
		if strings.TrimSpace(audit) == "" {
			audit = verdict.RenderMarkdown(publicVerdict)
		}
		investigationLog := fmt.Sprintf(
			"Scan %s of %s\nModel: %s\nPrompt version: %s\nBudget cap: $%.2f\nBudget spent: $%.4f\n",
			scanID, target, cfg.Models.Default, result.PromptVersion, cfg.Scan.BudgetUSD, budgetTracker.SpentUSD(),
		)
		if err := verdict.Write(scanDir, publicVerdict, audit, investigationLog); err != nil {
			log.Printf("serve: scan %s write artifacts failed: %v", scanID, err)
			runner.Emit(scanID, scanner.Event{Stage: "done", Status: "error", Message: err.Error()})
			runner.Complete(scanID)
			return
		}

		runner.Emit(scanID, scanner.Event{Stage: "done", Status: "complete", Message: publicVerdict.Verdict})
		runner.Complete(scanID)
	}
}

// buildRoutedStartScan is the default (MCP-mode) entry point. It routes each
// scan by the configured provider: Claude Code → the MCP-spawn path; a
// direct-API provider → the in-process client path. CLI agents other than
// claude-code (gemini-cli/codex-cli) are not wired yet and fail clearly.
func buildRoutedStartScan(paths *store.Paths, claudeBin string, pol *policy.Policy, reg *provider.Registry) api.StartScanFunc {
	direct := buildStartScan(paths, pol, reg)
	mcp := buildMCPStartScan(paths, claudeBin, pol)
	return func(ctx context.Context, scanID, target string, offline bool, since string, runner *api.ScanRunner) {
		cfg, _ := store.LoadConfig(paths.ConfigFile)
		id := provider.AgentID(cfg.Models.Provider).Resolve()
		switch {
		case id.IsCLI():
			// claude-code, cursor-agent, gemini-cli, codex-cli all drive the
			// assay MCP server; buildMCPStartScan resolves the specific agent
			// from config and fails clearly if its CLI has no adapter/binary.
			mcp(ctx, scanID, target, offline, since, runner)
		default: // -api direct providers (in-process)
			direct(ctx, scanID, target, offline, since, runner)
		}
	}
}

// buildFakeStartScan returns a StartScanFunc that replays recorded fixtures
// instead of calling Anthropic. Target basename → fixturesDir/<basename>.json.
// If no fixture matches, the scan fails fast with a clear error.
func buildFakeStartScan(paths *store.Paths, fixturesDir string, pol *policy.Policy) api.StartScanFunc {
	return func(ctx context.Context, scanID, target string, offline bool, since string, runner *api.ScanRunner) {

		var scanDir string
		fail := func(stage, msg string) {
			runner.Emit(scanID, scanner.Event{Stage: stage, Status: "error", Message: msg})
			if scanDir != "" {
				writeScanError(scanDir, scanID, target, stage, msg)
			}
			runner.Complete(scanID)
		}

		fixDir := fixturesDir
		if fixDir == "" {
			fixDir = defaultFixturesDir()
		}
		basename := filepath.Base(target)
		fixturePath := filepath.Join(fixDir, basename+".json")
		fixture, err := claude.LoadFixture(fixturePath)
		if err != nil {
			log.Printf("serve --fake: scan %s no fixture for target %s (%v)", scanID, target, err)
			fail("fixture", fmt.Sprintf("no recorded fixture at %s — add one or omit --fake", fixturePath))
			return
		}
		fakeClient := claude.NewFakeClientFromFixture(fixture)

		cfg, err := store.LoadConfig(paths.ConfigFile)
		if err != nil {
			log.Printf("serve --fake: scan %s load config failed: %v", scanID, err)
			fail("config", err.Error())
			return
		}

		history := store.NewHistory(paths.ScansDir)
		targetName := assaymcp.DeriveTargetName(target)
		allocatedDir, err := history.AllocateAt(targetName, scanID)
		if err != nil {
			log.Printf("serve --fake: scan %s allocate dir failed: %v", scanID, err)
			fail("prepass", err.Error())
			return
		}
		scanDir = allocatedDir

		evCh := make(chan scanner.Event, 64)
		bridgeDone := make(chan struct{})
		go func() {
			for e := range evCh {
				runner.Emit(scanID, e)
			}
			close(bridgeDone)
		}()

		result, scanErr := scanner.Scan(ctx, scanner.Options{
			Target:              target,
			Model:               cfg.Models.Default,
			ModelInvestigation:  cfg.Models.Investigation,
			BudgetUSD:           cfg.Scan.BudgetUSD,
			SubagentConcurrency: cfg.Scan.SubagentConcurrency,
			Offline:             offline,
		}, fakeClient, evCh)
		close(evCh)
		<-bridgeDone

		if scanErr != nil {
			log.Printf("serve --fake: scan %s failed: %v", scanID, scanErr)
			runner.Emit(scanID, scanner.Event{Stage: "done", Status: "error", Message: scanErr.Error()})
			writeScanError(scanDir, scanID, target, "scan", scanErr.Error())
			runner.Complete(scanID)
			return
		}

		result.ScanID = scanID
		validatedFindings, _ := verdict.Validate(target, convertToVerdictFindings(result.Findings))
		validatedFindings, denied := applyFloorAndPolicy(ctx, target, offline, pol, validatedFindings)
		publicVerdict := verdict.FromScanner(result, verdict.Target{
			Kind:   "claude-code-plugin",
			Name:   targetName,
			Source: "fixture://" + fixturePath,
		}, scannerVersion)
		publicVerdict.Findings = validatedFindings
		if publicVerdict.Findings == nil {
			publicVerdict.Findings = []verdict.Finding{}
		}
		publicVerdict.Verdict = recomputeVerdictFromValidated(validatedFindings)
		if denied {
			publicVerdict.Verdict = "unsafe"
			publicVerdict.Summary = policyDenyNote + publicVerdict.Summary
		}

		audit := result.AuditMarkdown
		if strings.TrimSpace(audit) == "" {
			audit = verdict.RenderMarkdown(publicVerdict)
		}
		investigationLog := fmt.Sprintf(
			"FAKE scan %s of %s\nFixture: %s\nExpected verdict: %s\n",
			scanID, target, fixturePath, fixture.ExpectedVerdict,
		)
		if err := verdict.Write(scanDir, publicVerdict, audit, investigationLog); err != nil {
			log.Printf("serve --fake: scan %s write artifacts failed: %v", scanID, err)
			runner.Emit(scanID, scanner.Event{Stage: "done", Status: "error", Message: err.Error()})
			runner.Complete(scanID)
			return
		}

		// Auto-diff parity with MCP mode: when caller requested a baseline,
		// post-process the audit.json on disk through verdict.Diff.
		if since != "" {
			if err := annotateWithDiff(paths.ScansDir, scanID, target, since); err != nil {
				log.Printf("serve --fake: scan %s auto-diff failed: %v", scanID, err)
			}
		}

		runner.Emit(scanID, scanner.Event{Stage: "done", Status: "complete", Message: publicVerdict.Verdict})
		runner.Complete(scanID)
	}
}

// defaultFixturesDir returns the in-repo testdata/recorded path, found by
// walking up from the binary's working directory looking for go.mod. Returns
// the absolute path if found, else "./testdata/recorded" as a last-ditch
// fallback so the user sees a sensible "no fixture at <path>" error.
func defaultFixturesDir() string {
	wd, err := os.Getwd()
	if err != nil {
		return filepath.Join("testdata", "recorded")
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "testdata", "recorded")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return filepath.Join(wd, "testdata", "recorded")
		}
		dir = parent
	}
}

// buildMCPStartScan returns a StartScanFunc that delegates scan execution to
// a Claude Code subprocess driving the assay MCP server. This is the v0.3
// architecture's default: scanning runs in Claude Code's context, using its
// subscription quota and prompt-cache management. The Go process here just:
//
//   - Allocates the scan directory + scan_id.
//   - Spawns `claude -p <methodology>` with --mcp-config wired to our binary.
//   - Tails events.jsonl (which the subprocess's assay_emit_progress writes)
//     and pumps each event into the in-memory runner so SSE subscribers see
//     progress identical to the legacy in-process mode.
//   - Surfaces subprocess errors as a final error event + error.json.
func buildMCPStartScan(paths *store.Paths, claudeBin string, pol *policy.Policy) api.StartScanFunc {
	return func(ctx context.Context, scanID, target string, offline bool, since string, runner *api.ScanRunner) {
		// scan dir allocated up-front so the tailer (started immediately) has
		// something to poll for. Failures here become a hard fail event.
		state := assaymcp.NewScanState(paths.ScansDir)
		scanDir, err := state.Allocate(scanID, target)
		if err != nil {
			log.Printf("serve --scan-mode=mcp: scan %s allocate failed: %v", scanID, err)
			runner.Emit(scanID, scanner.Event{Stage: "prepass", Status: "error", Message: err.Error()})
			runner.Complete(scanID)
			return
		}

		// Resolve the MCP-capable agent CLI for this scan (claude-code by
		// default; cursor-agent etc. when selected) and its API key from the
		// keychain, BEFORE starting the tailer so a misconfig fails cleanly.
		cfg, _ := store.LoadConfig(paths.ConfigFile)
		agentID := provider.AgentID(cfg.Models.Provider).Resolve()
		agentBin := ""
		if agentID == provider.AgentClaudeCode {
			agentBin = claudeBin
		}
		agent, agentErr := assaymcp.AgentFor(string(agentID), agentBin)
		if agentErr != nil {
			log.Printf("serve --scan-mode=mcp: scan %s agent: %v", scanID, agentErr)
			runner.Emit(scanID, scanner.Event{Stage: "prepass", Status: "error", Message: agentErr.Error()})
			writeScanError(scanDir, scanID, target, "config", agentErr.Error())
			runner.Complete(scanID)
			return
		}
		agentKey, _ := store.NewKeyring(configKeyringService).GetProviderKey(string(agentID))

		// Pump events.jsonl → runner so /api/scans/:id/stream subscribers see
		// per-stage progress live. Cancels with ctx; closes on terminal 'done'.
		// A WaitGroup lets us join the tailer before runner.Complete so it can
		// never emit to a completed (map-purged) scan id.
		tailCtx, cancelTail := context.WithCancel(ctx)
		defer cancelTail()
		var tailWG sync.WaitGroup
		tailWG.Add(1)
		go func() {
			defer tailWG.Done()
			for ev := range assaymcp.TailEvents(tailCtx, scanDir) {
				runner.Emit(scanID, scanner.Event{Stage: ev.Stage, Status: ev.Status, Message: ev.Message, At: ev.At})
			}
		}()

		// Find our own binary so the subprocess MCP server is the same build
		// as the parent. (Falls back to "assay" on PATH if exec lookup fails.)
		assayBin := "assay"
		if exe, err := os.Executable(); err == nil {
			assayBin = exe
		}

		// Stderr buffer so subprocess errors surface in the failure event.
		errBuf := &subprocessErrTail{lines: make([]string, 0, 32)}
		// Read the current configured model on every scan so live edits
		// from Settings take effect without a server restart. The user's
		// strict-honor expectation: if they pick Sonnet in the UI, the
		// next scan runs on Sonnet — no fallback to Claude Code defaults.
		modelOverride := cfg.Models.Default

		// Diff-mode: resume the prior scan's Claude session so the re-scan
		// reuses its context (threat model, prior reads) instead of starting
		// cold. Best-effort — an empty id just means a normal cold scan.
		resumeID := ""
		if since != "" {
			resumeID = loadPriorSessionID(paths.ScansDir, target, scanID, since)
		}

		var sessionID string
		spawnErr := assaymcp.SpawnScan(ctx, assaymcp.SpawnConfig{
			Agent:           agent,
			APIKey:          agentKey,
			AssayBin:        assayBin,
			Stderr:          errBuf,
			Offline:         offline,
			Model:           modelOverride,
			ResumeSessionID: resumeID,
			SessionIDOut:    &sessionID,
			Subagents:       cfg.Scan.DeepScan,
			OnStreamEvent: func(e assaymcp.StreamEvent) {
				// Surface the running cost on the terminal result so the live
				// UI can show what the scan spent of the user's quota.
				if e.Type == "result" && e.CostUSD > 0 {
					runner.Emit(scanID, scanner.Event{
						Stage:   "synthesis",
						Status:  "complete",
						Message: fmt.Sprintf("scan cost $%.4f", e.CostUSD),
					})
				}
			},
		}, target, scanID)

		// Persist the Claude session id so a later diff-mode scan can --resume it.
		if sessionID != "" {
			if data, err := json.Marshal(map[string]string{"session_id": sessionID}); err == nil {
				_ = os.WriteFile(filepath.Join(scanDir, "session.json"), data, 0o600)
			}
		}

		if spawnErr != nil {
			detail := spawnErr.Error()
			if tail := errBuf.String(); tail != "" {
				detail += " | claude stderr: " + tail
			}
			log.Printf("serve --scan-mode=mcp: scan %s spawn failed: %s", scanID, detail)
			runner.Emit(scanID, scanner.Event{Stage: "done", Status: "error", Message: detail})
			writeScanError(scanDir, scanID, target, "spawn", detail)
		} else if detail := unfinalizedScanFailure(scanDir, scanID, target, errBuf.String()); detail != "" {
			// Clean subprocess exit but no audit.json — finalize never ran
			// (commonly the agent hit --max-turns first). unfinalizedScanFailure
			// has already written error.json so the report/stream endpoints
			// resolve this to a reachable "failed" state instead of a 404.
			log.Printf("serve --scan-mode=mcp: scan %s exited without finalizing", scanID)
			runner.Emit(scanID, scanner.Event{Stage: "done", Status: "error", Message: detail})
		}
		// Note: a successful subprocess exit doesn't guarantee assay_finalize_scan
		// ran — it just means claude finished. The tailer keeps reading until
		// it sees stage=done or ctx ends, then closes. Final 'done' on
		// success is emitted by assay_finalize_scan inside the MCP server; the
		// branch above catches the case where it never came.

		// Give the tailer a brief grace period to flush any tail events that
		// arrived just before the subprocess exited, then stop it and JOIN it
		// before runner.Complete so it can never emit to a purged scan id.
		select {
		case <-ctx.Done():
		case <-time.After(500 * time.Millisecond):
		}
		cancelTail()
		tailWG.Wait()

		// Policy suppression / deny-category over the subprocess-written
		// audit.json (the SCA/poison floor already ran inside assembleVerdict).
		// Read-modify-write, mirroring annotateWithDiff. Best-effort.
		if spawnErr == nil && scanFinalized(scanDir) && pol != nil {
			if err := applyPolicyToAuditOnDisk(paths.ScansDir, scanID, pol); err != nil {
				log.Printf("serve --scan-mode=mcp: scan %s policy apply failed: %v", scanID, err)
			}
		}

		// Auto-diff: if the caller requested a baseline AND the scan produced
		// an audit.json, annotate findings relative to the prior scan and
		// rewrite audit.json. Errors are best-effort — the original audit is
		// already on disk so a failed diff doesn't break the scan.
		if spawnErr == nil && scanFinalized(scanDir) && since != "" {
			if err := annotateWithDiff(paths.ScansDir, scanID, target, since); err != nil {
				log.Printf("serve --scan-mode=mcp: scan %s auto-diff failed: %v", scanID, err)
				// Surface as an info event so the UI knows to render without
				// diff context.
				runner.Emit(scanID, scanner.Event{
					Stage:   "synthesis",
					Status:  "complete",
					Message: "diff baseline unavailable: " + err.Error(),
				})
			}
		}

		runner.Complete(scanID)
	}
}

// policyDenyNote prefixes the executive summary when a policy deny-category
// finding forces the verdict to unsafe in serve mode (which has no exit code
// to fail like the CLI gate does).
const policyDenyNote = "Note: a policy deny-category finding is present; Assay forced this verdict to unsafe.\n\n"

// applyFloorAndPolicy runs the deterministic floor (SCA/poison) then policy
// suppression over validated findings — the legacy/fake serve equivalent of
// the CLI's floor+policy step. Returns the surviving findings and whether a
// denied-category finding remains (the caller forces the verdict to unsafe).
func applyFloorAndPolicy(ctx context.Context, target string, offline bool, pol *policy.Policy, findings []verdict.Finding) ([]verdict.Finding, bool) {
	findings = floor.Apply(ctx, target, offline, findings)
	if pol == nil {
		return findings, false
	}
	kept, _ := pol.Apply(findings, time.Now())
	return kept, len(pol.DeniedCategoryHits(kept)) > 0
}

// applyPolicyToAuditOnDisk re-reads a finalized (MCP-mode) audit.json, applies
// policy suppression + deny-category enforcement, recomputes the verdict, and
// writes it back. MCP audits are produced by the subprocess, so the serving
// user's policy (resolved at boot) is applied here as a read-modify-write,
// mirroring annotateWithDiff. No-op when pol is nil or nothing changes.
func applyPolicyToAuditOnDisk(scansDir, scanID string, pol *policy.Policy) error {
	if pol == nil {
		return nil
	}
	dir, err := assaymcp.FindScanDir(scansDir, scanID)
	if err != nil {
		return fmt.Errorf("find scan: %w", err)
	}
	v, err := assaymcp.LoadVerdictFromDir(dir)
	if err != nil {
		return fmt.Errorf("load audit: %w", err)
	}
	kept, suppressed := pol.Apply(v.Findings, time.Now())
	denied := pol.DeniedCategoryHits(kept)
	if len(suppressed) == 0 && len(denied) == 0 {
		return nil // policy had no effect on this audit
	}
	v.Findings = kept
	if v.Findings == nil {
		v.Findings = []verdict.Finding{}
	}
	v.Verdict = recomputeVerdictFromValidated(kept)
	if len(denied) > 0 {
		v.Verdict = "unsafe"
		v.Summary = policyDenyNote + v.Summary
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal audit: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "audit.json"), data, 0o600); err != nil { // #nosec G306 -- private artifact
		return fmt.Errorf("write audit: %w", err)
	}
	return nil
}

// annotateWithDiff loads the just-completed audit.json, finds the prior scan
// loadPriorSessionID resolves the prior scan dir for a diff-mode scan (the
// same baseline annotateWithDiff uses) and returns its persisted Claude
// session id so the new scan can --resume it. Best-effort: any failure returns
// "" and the scan runs cold. `since` is "latest" or an explicit scan id;
// scanID is the current scan, excluded from "latest" resolution.
func loadPriorSessionID(scansDir, target, scanID, since string) string {
	var priorDir string
	var err error
	if since == "latest" {
		_, priorDir, err = assaymcp.FindPriorScan(scansDir, assaymcp.DeriveTargetName(target), scanID)
	} else {
		priorDir, err = assaymcp.FindScanDir(scansDir, since)
	}
	if err != nil || priorDir == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(priorDir, "session.json")) // #nosec G304 -- priorDir resolved under scansDir
	if err != nil {
		return ""
	}
	var s struct {
		SessionID string `json:"session_id"`
	}
	if json.Unmarshal(data, &s) != nil {
		return ""
	}
	return s.SessionID
}

// indicated by `since` ("latest" or an explicit scan_id), runs verdict.Diff,
// writes annotations back into audit.json. The on-disk file is the single
// source of truth, so this is a read-modify-write.
func annotateWithDiff(scansDir, scanID, target, since string) error {
	currentDir, err := assaymcp.FindScanDir(scansDir, scanID)
	if err != nil {
		return fmt.Errorf("find current scan: %w", err)
	}
	current, err := assaymcp.LoadVerdictFromDir(currentDir)
	if err != nil {
		return fmt.Errorf("load current audit: %w", err)
	}

	var priorDir, priorID string
	if since == "latest" {
		priorID, priorDir, err = assaymcp.FindPriorScan(scansDir, filepath.Base(target), scanID)
		if err != nil {
			return fmt.Errorf("find prior scan: %w", err)
		}
	} else {
		priorDir, err = assaymcp.FindScanDir(scansDir, since)
		if err != nil {
			return fmt.Errorf("resolve since=%q: %w", since, err)
		}
		priorID = since
	}
	prior, err := assaymcp.LoadVerdictFromDir(priorDir)
	if err != nil {
		return fmt.Errorf("load prior audit: %w", err)
	}

	annotated, _ := verdict.Diff(prior.Findings, current.Findings)
	// Tag the SinceScan field on each annotation so consumers know the baseline.
	for i := range annotated {
		if annotated[i].Diff != nil {
			annotated[i].Diff.SinceScan = priorID
		}
	}
	current.Findings = annotated
	current.PriorScanID = priorID

	auditPath := filepath.Join(currentDir, "audit.json")
	auditJSON, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal updated audit: %w", err)
	}
	if err := os.WriteFile(auditPath, auditJSON, 0o600); err != nil { // #nosec G306 -- private artifact
		return fmt.Errorf("write updated audit: %w", err)
	}
	return nil
}

// subprocessErrTail is an io.Writer that retains the last N lines of a stream.
// We use it to capture claude's stderr so spawn failures carry actionable text.
type subprocessErrTail struct {
	lines []string
}

func (t *subprocessErrTail) Write(p []byte) (int, error) {
	for _, line := range strings.Split(string(p), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		t.lines = append(t.lines, line)
		if len(t.lines) > 32 {
			t.lines = t.lines[len(t.lines)-32:]
		}
	}
	return len(p), nil
}

func (t *subprocessErrTail) String() string {
	if len(t.lines) == 0 {
		return ""
	}
	// Last 5 lines is usually enough context, full tail would spam SSE.
	start := 0
	if len(t.lines) > 5 {
		start = len(t.lines) - 5
	}
	return strings.Join(t.lines[start:], " ; ")
}

// scanFinalized reports whether the MCP subprocess produced a verdict for this
// scan. assay_finalize_scan writes audit.json synchronously before claude -p
// exits, so its absence after a clean subprocess exit means finalize never ran.
func scanFinalized(scanDir string) bool {
	if scanDir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(scanDir, "audit.json"))
	return err == nil
}

// unfinalizedScanFailure handles the case where the MCP subprocess exited
// cleanly (spawnErr == nil) but never wrote audit.json — it finished, commonly
// after exhausting --max-turns, before calling assay_finalize_scan. Such a scan
// has no terminal artifact, so /api/scans/:id 404s it and /api/scans/:id/stream
// reports "not active": the scan becomes a permanent, unreachable "pending"
// ghost (the "scan vanished after a refresh" bug). Writing an error.json here
// makes it resolve to a reachable "failed" state with a Retry path instead.
//
// Returns the failure detail (for the SSE event + log), or "" when audit.json
// is present and the scan finalized normally and no action is needed.
func unfinalizedScanFailure(scanDir, scanID, target, outputTail string) string {
	if scanFinalized(scanDir) {
		return ""
	}
	detail := "scan ended without producing a verdict — the agent finished or hit the turn limit (--max-turns) before calling assay_finalize_scan"
	if outputTail != "" {
		detail += " | claude output: " + outputTail
	}
	writeScanError(scanDir, scanID, target, "finalize", detail)
	return detail
}

// writeScanError persists a failure record into scanDir/error.json. The web UI
// reads this when audit.json is absent so the user sees the real failure reason
// instead of a generic 404. Best-effort: errors here are logged, not returned —
// the surrounding code is already on a failure path.
func writeScanError(scanDir, scanID, target, stage, message string) {
	if scanDir == "" {
		return
	}
	payload := map[string]any{
		"scan_id":   scanID,
		"target":    target,
		"stage":     stage,
		"error":     message,
		"failed_at": time.Now().UTC().Format(time.RFC3339Nano),
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		log.Printf("serve: scan %s error.json marshal failed: %v", scanID, err)
		return
	}
	errPath := filepath.Join(scanDir, "error.json")
	if err := os.WriteFile(errPath, data, 0o600); err != nil { // #nosec G306 -- private artifact dir
		log.Printf("serve: scan %s error.json write failed: %v", scanID, err)
	}
}
