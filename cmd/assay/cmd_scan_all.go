package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/chawdamrunal/assay/internal/fleet"
	"github.com/chawdamrunal/assay/internal/inventory"
	assaymcp "github.com/chawdamrunal/assay/internal/mcp"
	"github.com/chawdamrunal/assay/internal/server"
	"github.com/chawdamrunal/assay/internal/store"
)

// newScanAllCmd implements `assay scan-all`. It enumerates every installed
// Claude Code plugin, scans each in parallel through the MCP architecture,
// and writes a fleet-aggregate report. The whole reason this command exists
// is the value-prop pillar a developer can't reach with `git clone`: auditing
// 30 plugins at once is impossible by hand.
func newScanAllCmd() *cobra.Command {
	var parallel int
	var excludeFlag string
	var claudeBin string
	var claudeDir string
	var quick bool
	var failOn string

	cmd := &cobra.Command{
		Use:   "scan-all",
		Short: "Scan every installed Claude Code plugin and skill and aggregate the results",
		Long: `scan-all enumerates installed Claude Code plugins (from
~/.claude/plugins/installed_plugins.json), standalone skills (~/.claude/skills), and any
local connector manifests (~/.claude/connectors), filters out anything passed in
--exclude, and runs the MCP-mode scanner against each in parallel (default 2 workers).
Each scan produces an audit.json under ~/.assay/scans/<name>/<scan_id>/ as usual; the
fleet wrapper aggregates them into ~/.assay/fleet/<fleet_id>/report.json plus a live
events.jsonl. MCP servers, hooks, and settings have no local source tree to scan, and
purely-hosted connectors aren't locally discoverable.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			paths, err := resolvePaths()
			if err != nil {
				return err
			}
			if err := paths.Ensure(); err != nil {
				return err
			}
			if err := assaymcp.CheckClaudeAvailable(claudeBin); err != nil {
				return fmt.Errorf("claude CLI required for scan-all (--scan-mode mcp): %w", err)
			}

			// Resolve which plugins to scan.
			runtime := server.NewRuntime(paths, claudeDir)
			inv, err := runtime.LoadInventory()
			if err != nil {
				return fmt.Errorf("load inventory: %w", err)
			}
			excludes := splitCSV(excludeFlag)
			excludeSet := map[string]bool{}
			for _, e := range excludes {
				excludeSet[e] = true
			}

			members := []fleet.Member{}
			skipped := []string{}
			// Track plugin name per scan_id so the StartScan closure can pass
			// it through to ScanState.AllocateAs (fixes the bug where two
			// plugins sharing a version subdir collide because
			// filepath.Base(installPath) returns "1.0.0").
			nameByScanID := map[string]string{}
			for _, it := range inv.Items {
				// Plugins, standalone skills, and locally-bundled connectors are
				// the locally-scannable kinds (all carry a LocalPath). MCP-server
				// / hook / settings items have no source tree to scan.
				if it.Kind != inventory.KindClaudeCodePlugin &&
					it.Kind != inventory.KindSkill &&
					it.Kind != inventory.KindConnector {
					continue
				}
				if it.LocalPath == "" {
					skipped = append(skipped, it.Name+" (no local_path)")
					continue
				}
				if excludeSet[it.Name] {
					skipped = append(skipped, it.Name+" (excluded)")
					continue
				}
				scanID := uuid.NewString()
				nameByScanID[scanID] = it.Name
				members = append(members, fleet.Member{
					Target: it.LocalPath,
					ScanID: scanID,
				})
			}

			if len(members) == 0 {
				return fmt.Errorf("no plugins or skills to scan (inventory empty or all excluded)")
			}

			// Allocate the fleet on disk.
			fleetDir := filepath.Join(paths.DataDir, "fleet")
			if err := os.MkdirAll(fleetDir, 0o750); err != nil {
				return fmt.Errorf("mkdir fleet root: %w", err)
			}
			fStore := fleet.NewStore(fleetDir)
			fleetID := uuid.NewString()
			if _, err := fStore.Allocate(fleetID, members, excludes); err != nil {
				return fmt.Errorf("allocate fleet: %w", err)
			}

			// The --quick flag is parsed but not yet wired into the per-member
			// scan dispatch (runner.Run does not currently expose a quick-mode
			// hook into SpawnScan). v0.6 Phase B wires this through the
			// methodology-triage prompt. Until then we emit a warning rather
			// than silently dropping the user's intent.
			if quick {
				fmt.Fprintln(cmd.ErrOrStderr(),
					"warning: --quick has no effect on scan-all yet (wiring lands in v0.6); falling back to full scans")
			}
			fmt.Fprintf(cmd.OutOrStdout(),
				"Fleet %s starting: %d plugin(s), parallel=%d, mode=mcp\n",
				fleetID, len(members), parallel)
			for _, s := range skipped {
				fmt.Fprintf(cmd.OutOrStdout(), "  skip: %s\n", s)
			}
			fmt.Fprintln(cmd.OutOrStdout())

			// Print a live one-line-per-event summary.
			broadcaster := fleet.NewBroadcaster()
			ch, unsub := broadcaster.Subscribe(256)
			defer unsub()
			go printFleetTicker(ch, members, cmd.ErrOrStderr())

			// StartScan closure: same shape as buildMCPStartScan minus the
			// HTTP ScanRunner. Each call spawns one claude -p subprocess and
			// blocks until it exits.
			assayBin := selfBin()
			start := func(ctx context.Context, scanID, target string, offline bool, _ string) {
				_ = offline
				state := assaymcp.NewScanState(paths.ScansDir)
				// Pass the inventory-derived plugin name so the scan dir lands
				// under .../<plugin-name>/<scan_id>/, not .../<version>/<scan_id>/.
				scanDir, err := state.AllocateAs(scanID, target, nameByScanID[scanID])
				if err != nil {
					log.Printf("scan-all: allocate %s failed: %v", target, err)
					return
				}
				errBuf := &subprocessErrTail{lines: make([]string, 0, 32)}
				spawnErr := assaymcp.SpawnScan(ctx, assaymcp.SpawnConfig{
					ClaudeBin: claudeBin,
					AssayBin:  assayBin,
					Stderr:    errBuf,
				}, target, scanID)
				if spawnErr != nil {
					detail := spawnErr.Error()
					if tail := errBuf.String(); tail != "" {
						detail += " | " + tail
					}
					writeScanError(scanDir, scanID, target, "spawn", detail)
				}
			}

			runner := fleet.NewRunner(fStore, paths.ScansDir, broadcaster)
			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()
			rep, err := runner.Run(ctx, fleetID, members, parallel, false, start)
			broadcaster.Close()
			if err != nil {
				return fmt.Errorf("fleet run: %w", err)
			}

			// Final tabular summary.
			fmt.Fprintln(cmd.OutOrStdout())
			fmt.Fprintf(cmd.OutOrStdout(), "Fleet %s complete.\n", fleetID)
			fmt.Fprintf(cmd.OutOrStdout(),
				"  Verdicts: safe=%d caution=%d unsafe=%d\n",
				rep.Verdict.Safe, rep.Verdict.Caution, rep.Verdict.Unsafe)
			fmt.Fprintf(cmd.OutOrStdout(),
				"  Severity: critical=%d high=%d medium=%d low=%d info=%d\n",
				rep.Severity.Critical, rep.Severity.High, rep.Severity.Medium,
				rep.Severity.Low, rep.Severity.Info)
			fmt.Fprintf(cmd.OutOrStdout(), "  Report: %s\n", filepath.Join(fleetDir, fleetID, "report.json"))
			fmt.Fprintf(cmd.OutOrStdout(), "  Browse: assay serve → http://localhost:7373/fleet/%s\n", fleetID)

			// CI gate: exit 2 when any plugin in the fleet meets the threshold.
			if gate := evalFleetFailOn(rep, failOn); gate != nil {
				fmt.Fprintln(cmd.OutOrStdout(), gate.msg)
				return gate
			}
			return nil
		},
	}

	cmd.Flags().IntVar(&parallel, "parallel", 2, "Concurrent scans (default 2; raise for API-key users, keep low for Claude Code subscription quotas)")
	cmd.Flags().StringVar(&excludeFlag, "exclude", "", "Comma-separated plugin names to skip")
	cmd.Flags().StringVar(&claudeBin, "claude-bin", "claude", "Path to the Claude Code CLI")
	cmd.Flags().StringVar(&claudeDir, "claude-dir", "", "Path to ~/.claude (overrides default)")
	cmd.Flags().BoolVar(&quick, "quick", false, "Use the QuickProfile (tier-1 triage only); deep scans are kicked off in background")
	// The flag stays declared so existing scripts don't break, but it is hidden
	// from --help to avoid advertising behaviour the runner does not yet
	// implement. Wiring lands in v0.6 Phase B alongside the methodology-triage
	// prompt path.
	_ = cmd.Flags().MarkHidden("quick")
	cmd.Flags().StringVar(&failOn, "fail-on", "unsafe", "Exit code 2 when any plugin meets this threshold: unsafe | caution | any | off")
	return cmd
}

// evalFleetFailOn returns a non-nil *exitCodeError (code 2) when the fleet
// report meets the requested CI gate threshold. Mirrors evalFailOn but over
// aggregate fleet counts.
func evalFleetFailOn(rep *fleet.Report, failOn string) *exitCodeError {
	if rep == nil {
		return nil
	}
	trigger := false
	switch strings.ToLower(strings.TrimSpace(failOn)) {
	case "off", "never", "none":
		trigger = false
	case "any":
		trigger = rep.Severity.Critical+rep.Severity.High+rep.Severity.Medium+rep.Severity.Low+rep.Severity.Info > 0
	case "caution":
		trigger = rep.Verdict.Caution > 0 || rep.Verdict.Unsafe > 0
	default: // "", "unsafe", or unrecognized → unsafe
		trigger = rep.Verdict.Unsafe > 0
	}
	if !trigger {
		return nil
	}
	return &exitCodeError{
		code: 2,
		msg:  fmt.Sprintf("assay: %d unsafe / %d caution plugin(s) meet --fail-on=%s threshold (exit 2)", rep.Verdict.Unsafe, rep.Verdict.Caution, failOn),
	}
}

// printFleetTicker prints one terminal line per Event so the user sees
// progress without having to open the web UI. Format:
//
//	[HH:MM:SS] <plugin>: stage=<stage> status=<status>
func printFleetTicker(events <-chan fleet.Event, members []fleet.Member, w io.Writer) {
	idToTarget := map[string]string{}
	for _, m := range members {
		idToTarget[m.ScanID] = filepath.Base(m.Target)
	}
	for ev := range events {
		if ev.ScanID == "" && ev.Stage == "fleet" {
			fmt.Fprintf(w, "[%s] fleet: %s\n", time.Now().Format("15:04:05"), ev.Status)
			continue
		}
		target := idToTarget[ev.ScanID]
		if target == "" {
			target = ev.ScanID[:8]
		}
		msg := ""
		if ev.Message != "" {
			msg = "  " + ev.Message
		}
		fmt.Fprintf(w, "[%s] %s: %s/%s%s\n", time.Now().Format("15:04:05"), target, ev.Stage, ev.Status, msg)
	}
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// selfBin returns the absolute path of the running assay binary so the
// spawned claude subprocess uses the same build for its MCP server. Falls
// back to "assay" on PATH if exec.LookPath fails.
func selfBin() string {
	if exe, err := os.Executable(); err == nil {
		return exe
	}
	return "assay"
}

// Ensure unused imports stay used in this file (kept stable across edits).
var _ = errors.New
var _ = store.NewHistory
