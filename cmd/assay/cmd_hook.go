package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

//go:embed hooks/assay-pre-install.sh
var preInstallHookScript []byte

// newHookCmd registers `assay hook install|uninstall|status|resolve`. This is
// the v0.4 pre-install gate surface — a UserPromptSubmit hook that intercepts
// `/plugin install <ref>` in Claude Code, runs the quick deterministic scan,
// and returns a permissionDecision so the install can be allowed / asked /
// denied before it commits.
//
// We chose UserPromptSubmit (vs PreToolUse) deliberately: `/plugin install`
// is a slash command, not a tool call, so PreToolUse cannot intercept it.
// Research confirmed via `~/.claude/plugins/marketplaces/.../plugin-dev/...`.
func newHookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hook",
		Short: "Manage the Assay pre-install gate hook in Claude Code's settings.json",
	}
	cmd.AddCommand(newHookInstallCmd())
	cmd.AddCommand(newHookUninstallCmd())
	cmd.AddCommand(newHookStatusCmd())
	cmd.AddCommand(newHookResolveCmd())
	return cmd
}

// hookMarker tags entries this command added to settings.json so we can
// safely remove them later without disturbing hooks the user added by hand.
const hookMarker = "managed-by:assay"

// settingsFile is the global Claude Code settings file path. Per-project
// settings live elsewhere and are out of scope for the global gate.
func settingsFile() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

func scriptPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".assay", "hooks", "assay-pre-install.sh"), nil
}

func newHookInstallCmd() *cobra.Command {
	var timeout int
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install the pre-install gate as a UserPromptSubmit hook",
		Long: `install writes ~/.assay/hooks/assay-pre-install.sh and adds a UserPromptSubmit
entry to ~/.claude/settings.json that runs the script before every prompt.

The script fast-exits on prompts that are not "/plugin install <ref>", so the
overhead on regular prompts is sub-millisecond. On install commands, it runs
assay scan --quick (no LLM call, deterministic pre-pass) and returns a
permissionDecision via the hookSpecificOutput protocol.

Idempotent: re-running install replaces an existing managed entry rather than
duplicating it. Hooks added by hand (lacking the managed-by:assay marker)
are left untouched.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			scriptDest, err := scriptPath()
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(scriptDest), 0o750); err != nil {
				return fmt.Errorf("mkdir hook dir: %w", err)
			}
			if err := os.WriteFile(scriptDest, preInstallHookScript, 0o750); err != nil { // #nosec G306 -- executable
				return fmt.Errorf("write script: %w", err)
			}

			settingsPath, err := settingsFile()
			if err != nil {
				return err
			}
			if err := upsertHook(settingsPath, scriptDest, timeout); err != nil {
				return fmt.Errorf("upsert hook in settings.json: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Installed pre-install gate.\n")
			fmt.Fprintf(cmd.OutOrStdout(), "  Script:   %s\n", scriptDest)
			fmt.Fprintf(cmd.OutOrStdout(), "  Settings: %s\n", settingsPath)
			fmt.Fprintf(cmd.OutOrStdout(), "  Timeout:  %ds\n\n", timeout)
			fmt.Fprintf(cmd.OutOrStdout(), "Restart Claude Code or open a new session for the hook to take effect.\n")
			return nil
		},
	}
	cmd.Flags().IntVar(&timeout, "timeout", 120, "Hook timeout in seconds (Claude Code default is 60; quick scan runs in <2s for most plugins)")
	return cmd
}

func newHookUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the pre-install gate from settings.json and disk",
		RunE: func(cmd *cobra.Command, _ []string) error {
			settingsPath, err := settingsFile()
			if err != nil {
				return err
			}
			removed, err := removeHook(settingsPath)
			if err != nil {
				return err
			}
			scriptDest, err := scriptPath()
			if err == nil {
				if rmErr := os.Remove(scriptDest); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
					fmt.Fprintf(cmd.ErrOrStderr(), "warn: could not remove script %s: %v\n", scriptDest, rmErr)
				}
			}
			if removed {
				fmt.Fprintln(cmd.OutOrStdout(), "Removed Assay pre-install gate.")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "No Assay-managed hook found in settings.json — nothing to remove.")
			}
			return nil
		},
	}
}

func newHookStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show current install state of the pre-install gate",
		RunE: func(cmd *cobra.Command, _ []string) error {
			settingsPath, _ := settingsFile()
			scriptDest, _ := scriptPath()

			scriptOK := false
			if info, err := os.Stat(scriptDest); err == nil && info.Mode().IsRegular() {
				scriptOK = true
			}
			hookFound, hookCmd, hookTimeout := readManagedHook(settingsPath)

			fmt.Fprintf(cmd.OutOrStdout(), "Assay pre-install gate:\n")
			fmt.Fprintf(cmd.OutOrStdout(), "  Script path:      %s (%s)\n", scriptDest, ifBool(scriptOK, "present", "missing"))
			fmt.Fprintf(cmd.OutOrStdout(), "  settings.json:    %s\n", settingsPath)
			fmt.Fprintf(cmd.OutOrStdout(), "  Hook installed:   %s\n", ifBool(hookFound, "yes", "no"))
			if hookFound {
				fmt.Fprintf(cmd.OutOrStdout(), "  Hook command:     %s\n", hookCmd)
				fmt.Fprintf(cmd.OutOrStdout(), "  Hook timeout:     %ds\n", hookTimeout)
			}
			return nil
		},
	}
}

func newHookResolveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resolve <plugin-ref>",
		Short: "Resolve a plugin reference (name@marketplace or name) to its on-disk source",
		Long: `Used by the gate script: given "frontend-design@claude-plugins-official"
or just "frontend-design", returns the absolute path under
~/.claude/plugins/marketplaces/<marketplace>/plugins/<name>/ (preferred — that
holds the source committed to the marketplace) or, if that is not present, the
cached install path under ~/.claude/plugins/cache/<marketplace>/<name>/<v>/.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := resolvePluginRef(args[0])
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), path)
			return nil
		},
	}
}

// resolvePluginRef parses "name@marketplace" or "name" and locates a source
// directory on disk. Returns an error if no candidate exists.
func resolvePluginRef(ref string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	name := ref
	marketplace := ""
	if i := strings.LastIndex(ref, "@"); i > 0 {
		name = ref[:i]
		marketplace = ref[i+1:]
	}

	roots := []string{}
	pluginsDir := filepath.Join(home, ".claude", "plugins")
	if marketplace != "" {
		// Preferred: marketplace source (always present once the marketplace
		// is added; contains the upstream-committed version of the plugin).
		roots = append(roots, filepath.Join(pluginsDir, "marketplaces", marketplace, "plugins", name))
		// Fallback: latest cached install.
		roots = append(roots, latestCacheDir(filepath.Join(pluginsDir, "cache", marketplace, name)))
	} else {
		// Unknown marketplace — search every marketplace for the name.
		mktDir := filepath.Join(pluginsDir, "marketplaces")
		mkts, _ := os.ReadDir(mktDir)
		for _, m := range mkts {
			if !m.IsDir() {
				continue
			}
			roots = append(roots, filepath.Join(mktDir, m.Name(), "plugins", name))
		}
		cacheDir := filepath.Join(pluginsDir, "cache")
		caches, _ := os.ReadDir(cacheDir)
		for _, c := range caches {
			if !c.IsDir() {
				continue
			}
			roots = append(roots, latestCacheDir(filepath.Join(cacheDir, c.Name(), name)))
		}
	}
	for _, candidate := range roots {
		if candidate == "" {
			continue
		}
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("plugin source not found for %q (looked in marketplaces + cache)", ref)
}

// latestCacheDir returns the lex-largest subdir of versionDir, or "" if none.
// Cached installs are versioned: cache/<mkt>/<name>/<version-or-sha>/.
func latestCacheDir(versionDir string) string {
	entries, err := os.ReadDir(versionDir)
	if err != nil {
		return ""
	}
	names := []string{}
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return ""
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	return filepath.Join(versionDir, names[0])
}

// --- settings.json read-modify-write -----------------------------------------

// settingsRoot is the parsed shape of ~/.claude/settings.json. We only read
// the hooks node here; other settings are preserved via a raw json.RawMessage
// map to avoid clobbering unknown keys on round-trip.
type settingsRoot map[string]json.RawMessage

func loadSettings(path string) (settingsRoot, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- known config file
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return settingsRoot{}, nil
		}
		return nil, err
	}
	out := settingsRoot{}
	if len(strings.TrimSpace(string(data))) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parse settings.json: %w", err)
	}
	return out, nil
}

func saveSettings(path string, root settingsRoot) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".settings-*.json")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(root); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// hookCommandEntry is one item in hooks.<EventName>[].hooks[].
type hookCommandEntry struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
	Marker  string `json:"managed_by,omitempty"`
}

// hookMatcherEntry is one item in hooks.<EventName>[].
type hookMatcherEntry struct {
	Matcher string             `json:"matcher,omitempty"`
	Hooks   []hookCommandEntry `json:"hooks"`
}

type hooksByEvent map[string][]hookMatcherEntry

func upsertHook(settingsPath, scriptDest string, timeoutSec int) error {
	root, err := loadSettings(settingsPath)
	if err != nil {
		return err
	}
	var hooks hooksByEvent
	if raw, ok := root["hooks"]; ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &hooks); err != nil {
			return fmt.Errorf("settings.hooks is not the expected shape: %w", err)
		}
	}
	if hooks == nil {
		hooks = hooksByEvent{}
	}

	// Build the entry we want to install.
	want := hookMatcherEntry{
		Matcher: ".*",
		Hooks: []hookCommandEntry{{
			Type:    "command",
			Command: scriptDest,
			Timeout: timeoutSec,
			Marker:  hookMarker,
		}},
	}

	// Strip any existing managed entry from UserPromptSubmit, then append ours.
	existing := hooks["UserPromptSubmit"]
	filtered := existing[:0]
	for _, e := range existing {
		if !entryIsManaged(e) {
			filtered = append(filtered, e)
		}
	}
	filtered = append(filtered, want)
	hooks["UserPromptSubmit"] = filtered

	encoded, err := json.MarshalIndent(hooks, "", "  ")
	if err != nil {
		return err
	}
	root["hooks"] = encoded
	return saveSettings(settingsPath, root)
}

func removeHook(settingsPath string) (bool, error) {
	root, err := loadSettings(settingsPath)
	if err != nil {
		return false, err
	}
	raw, ok := root["hooks"]
	if !ok || len(raw) == 0 {
		return false, nil
	}
	var hooks hooksByEvent
	if err := json.Unmarshal(raw, &hooks); err != nil {
		return false, fmt.Errorf("settings.hooks is not the expected shape: %w", err)
	}
	existing := hooks["UserPromptSubmit"]
	if len(existing) == 0 {
		return false, nil
	}
	filtered := existing[:0]
	removed := false
	for _, e := range existing {
		if entryIsManaged(e) {
			removed = true
			continue
		}
		filtered = append(filtered, e)
	}
	if !removed {
		return false, nil
	}
	if len(filtered) == 0 {
		delete(hooks, "UserPromptSubmit")
	} else {
		hooks["UserPromptSubmit"] = filtered
	}
	encoded, err := json.MarshalIndent(hooks, "", "  ")
	if err != nil {
		return false, err
	}
	root["hooks"] = encoded
	return removed, saveSettings(settingsPath, root)
}

func entryIsManaged(e hookMatcherEntry) bool {
	for _, h := range e.Hooks {
		if h.Marker == hookMarker {
			return true
		}
	}
	return false
}

func readManagedHook(settingsPath string) (found bool, command string, timeoutSec int) {
	root, err := loadSettings(settingsPath)
	if err != nil {
		return false, "", 0
	}
	raw, ok := root["hooks"]
	if !ok || len(raw) == 0 {
		return false, "", 0
	}
	var hooks hooksByEvent
	if err := json.Unmarshal(raw, &hooks); err != nil {
		return false, "", 0
	}
	for _, e := range hooks["UserPromptSubmit"] {
		for _, h := range e.Hooks {
			if h.Marker == hookMarker {
				return true, h.Command, h.Timeout
			}
		}
	}
	return false, "", 0
}

func ifBool(b bool, t, f string) string {
	if b {
		return t
	}
	return f
}

// keep time import used (referenced for future status enrichment).
var _ = time.Now
