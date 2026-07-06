package api

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrPathNotAllowed is returned when EnsureAllowed rejects a target. The
// API handler surfaces this as HTTP 403 with a fixed message; we deliberately
// do NOT echo the rejected path back so an attacker can't probe for which
// paths exist (information disclosure).
var ErrPathNotAllowed = errors.New("target path not allowed")

// EnsureAllowed verifies that `target` resolves to a path inside one of
// `allowedRoots` after symlink resolution. The clean absolute path is
// returned on success; otherwise ErrPathNotAllowed is returned.
//
// Why this exists: POST /api/scans accepts a `target` field that is passed
// straight to a Claude subprocess which reads files at that path. Without
// this guard a cross-origin request from any tab the user has open could
// scan /etc/passwd, ~/.ssh, or arbitrary system paths and exfiltrate
// contents through the LLM analysis. The guard pairs with the CSRF check
// in csrf.go — defense in depth.
//
// Allowed-roots model: the caller passes a whitelist of directories. For
// `assay serve` the whitelist is the user's plugin dirs (~/.claude/plugins,
// ~/.claude/plugins/marketplaces) plus the configured workspace dirs. The
// OS temp dir is added in test environments so unit tests can synthesise
// fixtures.
//
// Symlink behaviour: we resolve symlinks before the prefix check so a
// symlink under an allowed root cannot escape it. If the path doesn't
// exist (yet), we still resolve as much as we can with filepath.Abs +
// filepath.Clean — a non-existent path that lexically lives under an
// allowed root is allowed (the scan will fail later when the file isn't
// readable, which is a fine failure mode).
func EnsureAllowed(target string, allowedRoots []string) (string, error) {
	if strings.TrimSpace(target) == "" {
		return "", fmt.Errorf("%w: target is empty", ErrPathNotAllowed)
	}
	if len(allowedRoots) == 0 {
		// Fail-closed: if no roots are configured, reject everything. Callers
		// that intentionally want any-path scanning should pass [/].
		return "", fmt.Errorf("%w: no allowed roots configured", ErrPathNotAllowed)
	}

	// 1. Make the target absolute (resolves "." and relative paths).
	abs, err := filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrPathNotAllowed, err)
	}
	cleaned := filepath.Clean(abs)

	// 2. Resolve symlinks. EvalSymlinks fails on non-existent paths, so we
	// walk up the path until we find an existing ancestor, resolve THAT, and
	// re-append the unresolved tail. This handles two real cases:
	//   - macOS /var → /private/var symlinks (the temp dir is /var/folders/...)
	//   - user-supplied paths that don't exist yet (scanner will surface the
	//     missing-file error later; the guard only cares about lexical safety).
	resolved := resolveSymlinksAllowingMissing(cleaned)

	// 3. Check both the user-supplied (cleaned) form AND the symlink-resolved
	// form against each allowed root. The cleaned check guards against
	// lexical traversal (../../etc); the resolved check guards against
	// symlink escape from inside an allowed dir.
	for _, root := range allowedRoots {
		rootAbs, err := filepath.Abs(filepath.Clean(root))
		if err != nil {
			continue
		}
		rootResolved := rootAbs
		if r, err := filepath.EvalSymlinks(rootAbs); err == nil {
			rootResolved = r
		}
		if isUnder(cleaned, rootAbs) && isUnder(resolved, rootResolved) {
			return cleaned, nil
		}
	}
	return "", ErrPathNotAllowed
}

// resolveSymlinksAllowingMissing returns the symlink-resolved form of `p`
// even when `p` does not exist. It walks up the path until it finds an
// existing ancestor, resolves THAT, then re-appends the missing tail.
//
// This matters for two reasons: (a) macOS's /var and /tmp are symlinks to
// /private/var and /private/tmp, so a path like /var/folders/.../missing
// must still resolve under /private/var to pass the allowed-roots check.
// (b) Users may scan paths that haven't been created yet; the guard's job is
// lexical safety, not existence.
func resolveSymlinksAllowingMissing(p string) string {
	resolved, err := filepath.EvalSymlinks(p)
	if err == nil {
		return resolved
	}
	// Walk up until something exists. Bounded by path depth so never infinite.
	parent := p
	var tail []string
	for {
		next := filepath.Dir(parent)
		if next == parent { // reached root
			return p // give up — return the cleaned form unchanged
		}
		tail = append([]string{filepath.Base(parent)}, tail...)
		parent = next
		if r, err := filepath.EvalSymlinks(parent); err == nil {
			return filepath.Join(append([]string{r}, tail...)...)
		}
	}
}

// isUnder returns true when `child` is the same as `parent` or lives inside
// it. Both must be absolute, cleaned paths.
func isUnder(child, parent string) bool {
	if child == parent {
		return true
	}
	// Append separator so /home/foo is not mistakenly considered under /home/foobar.
	parentWithSep := parent
	if !strings.HasSuffix(parentWithSep, string(filepath.Separator)) {
		parentWithSep += string(filepath.Separator)
	}
	return strings.HasPrefix(child, parentWithSep)
}

// DefaultAllowedRoots returns the path roots assay serve trusts by default:
// the user's ~/.claude/plugins directory tree (which contains both installed
// plugins and the marketplaces cache) plus the current working directory for
// `assay scan .` style invocations.
//
// The OS temp dir is deliberately NOT included. On macOS /tmp (and the
// /var/folders temp tree) is world-writable, so trusting it would let any
// local process — or a malicious plugin — drop a target at a predictable
// temp path and have assay serve scan it (and exfiltrate its contents via
// the LLM analysis) past the path guard. Tests that need a temp root pass
// [t.TempDir()] explicitly to EnsureAllowed rather than relying on this
// default.
func DefaultAllowedRoots(claudeDir string) []string {
	roots := []string{}
	if claudeDir != "" {
		roots = append(roots, filepath.Join(claudeDir, "plugins"))
	}
	if cwd, err := os.Getwd(); err == nil {
		roots = append(roots, cwd)
	}
	return roots
}
