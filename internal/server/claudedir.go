package server

import (
	"os"
	"path/filepath"
	"runtime"
)

// ResolveClaudeDir picks the best ~/.claude (or equivalent) directory for
// the current OS, falling back to a sensible default when the probe finds
// nothing. The probe is cheap (filesystem Stat per candidate) and runs at
// startup only.
//
// macOS / Linux: ~/.claude.
// Windows:       %APPDATA%\Claude  (preferred by Claude Code on Windows)
//
//	~/.claude          (POSIX-style fallback for Git Bash / WSL users)
//
// Each candidate is preferred only if it contains a recognisable Claude
// Code subdir (plugins/, settings.json, or commands/). This avoids picking
// an empty dir that happens to exist for unrelated reasons.
//
// Returns the first candidate that looks like a real Claude Code install,
// or the OS default if none look populated.
func ResolveClaudeDir() string {
	candidates := defaultClaudeDirCandidates()
	for _, c := range candidates {
		if looksLikeClaudeDir(c) {
			return c
		}
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return ""
}

// defaultClaudeDirCandidates returns the OS-appropriate search order. The
// first match wins. Order matters: we want the OS-native location first,
// then portable fallbacks.
func defaultClaudeDirCandidates() []string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "windows":
		appdata := os.Getenv("APPDATA")
		out := []string{}
		if appdata != "" {
			out = append(out, filepath.Join(appdata, "Claude"))
		}
		if home != "" {
			out = append(out, filepath.Join(home, ".claude"))
		}
		return out
	default: // darwin, linux, *bsd
		if home == "" {
			return nil
		}
		return []string{filepath.Join(home, ".claude")}
	}
}

// looksLikeClaudeDir reports whether `dir` contains at least one of the
// subdirs Claude Code populates on first run.
func looksLikeClaudeDir(dir string) bool {
	if dir == "" {
		return false
	}
	for _, marker := range []string{"plugins", "settings.json", "commands"} {
		if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
			return true
		}
	}
	return false
}
