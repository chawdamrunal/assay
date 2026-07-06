// Package github fetches plugin source from GitHub on demand. This is the
// v0.6 capability that lets Assay answer "is github.com/x/y safe?" by
// cloning the repo to a quarantined cache dir and pointing the scanner at
// the local checkout — without asking the user to git-clone manually first.
//
// Security posture:
//
//  1. The clone subprocess runs with GIT_TERMINAL_PROMPT=0 so a private
//     repo without auth fails fast instead of hanging the HTTP handler.
//  2. Submodules are disabled — they're a recursive credential-disclosure
//     vector and irrelevant to the static scan we'll run downstream.
//  3. Symlinks are disabled at clone time (core.symlinks=false) so a
//     malicious repo cannot plant a symlink that escapes the cache root.
//  4. GIT_CONFIG_GLOBAL is set to /dev/null so the user's gitconfig (and
//     any hooks it might inject) doesn't run in our subprocess.
//  5. Every cloned dir is under ~/.assay/sources/ (the cache root passed
//     in by the caller) — the API path-guard subsequently restricts which
//     directories can be scanned, so even if a clone produces something
//     surprising it can only be scanned within the configured roots.
package github

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// reSafeRefSegment validates an owner / repo / git-ref segment before we let
// it anywhere near a shell. GitHub itself enforces a similar character set,
// but we re-check because the caller-supplied value flows from a chat
// message which we cannot trust.
var reSafeRefSegment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$`)

// ErrInvalidRef is returned when owner or repo contains characters that
// would be unsafe to pass to `git clone`. Sentinel so callers can map to
// HTTP 400 instead of 500.
var ErrInvalidRef = errors.New("invalid github owner/repo")

// CloneResult describes a successful clone.
type CloneResult struct {
	// LocalPath is the absolute path of the cloned tree, ready to be scanned.
	LocalPath string
	// CommitSHA is the resolved HEAD commit hash (full 40 chars) — used as
	// part of the cache directory name so two clones of the same repo at
	// different commits do not collide.
	CommitSHA string
	// CloneURL is the canonical URL that was cloned (https form).
	CloneURL string
}

// TokenFunc lazily resolves a GitHub token and a short source label. Clone
// calls it only after an anonymous clone fails, so a public repo never carries
// a credential. A nil TokenFunc — or one returning "" — disables the
// authenticated retry.
type TokenFunc func() (token, source string)

// Clone shallow-clones github.com/<owner>/<repo> into a quarantined subdir
// of `cacheRoot` (typically ~/.assay/sources/). When the same (owner, repo)
// is already cloned and the cache is fresh (< 1h), the existing path is
// returned without re-cloning.
//
// Returns ErrInvalidRef when owner or repo fails the safe-segment check.
// Otherwise returns the wrapped exec error so callers can surface the
// reason (404, auth required, network failure) to the user verbatim.
func Clone(ctx context.Context, owner, repo, cacheRoot string, tokenFn TokenFunc) (*CloneResult, error) {
	owner = strings.TrimSpace(owner)
	repo = strings.TrimSpace(repo)
	if !reSafeRefSegment.MatchString(owner) || !reSafeRefSegment.MatchString(repo) {
		return nil, fmt.Errorf("%w: %q/%q", ErrInvalidRef, owner, repo)
	}
	if cacheRoot == "" {
		return nil, fmt.Errorf("cacheRoot required")
	}
	if err := os.MkdirAll(cacheRoot, 0o750); err != nil {
		return nil, fmt.Errorf("mkdir cache root: %w", err)
	}

	cloneURL := fmt.Sprintf("https://github.com/%s/%s.git", owner, repo)

	// Cache key derived from owner+repo so concurrent scans of the same
	// repo land in the same dir. A short hash suffix keeps the dir name
	// unique even if owner/repo would otherwise collide across cases.
	hash := sha256.Sum256([]byte(owner + "/" + repo))
	cacheKey := strings.ToLower(owner) + "-" + strings.ToLower(repo) + "-" + hex.EncodeToString(hash[:4])
	destDir := filepath.Join(cacheRoot, cacheKey)

	// Fast path: existing fresh clone — skip git entirely.
	if info, err := os.Stat(filepath.Join(destDir, ".git")); err == nil && info.IsDir() {
		if cacheStat, err := os.Stat(destDir); err == nil &&
			time.Since(cacheStat.ModTime()) < time.Hour {
			sha, _ := readHeadSHA(destDir)
			return &CloneResult{LocalPath: destDir, CommitSHA: sha, CloneURL: cloneURL}, nil
		}
		// Stale — nuke the dir and re-clone. We don't try to `git pull`
		// because the user's intent is "scan the current HEAD"; a fresh
		// clone is the most predictable way to honor that.
		if err := os.RemoveAll(destDir); err != nil {
			return nil, fmt.Errorf("remove stale cache: %w", err)
		}
	} else if _, err := os.Stat(destDir); err == nil {
		// Dir exists but no .git — leftover failed clone. Clean up.
		_ = os.RemoveAll(destDir)
	}

	// Attempt an anonymous clone first so a public repo never carries a
	// credential (least privilege). Only if that fails do we resolve a token
	// and retry — that is the private-repo path.
	if err := runClone(ctx, cloneURL, destDir); err != nil {
		var token string
		if tokenFn != nil {
			token, _ = tokenFn()
		}
		if token == "" {
			return nil, err // no auth available; surface the anonymous failure
		}
		_ = os.RemoveAll(destDir) // clean the partial checkout before retrying
		if rerr := runCloneAuth(ctx, cloneURL, destDir, token); rerr != nil {
			return nil, redactToken(rerr, token)
		}
	}

	sha, _ := readHeadSHA(destDir)
	return &CloneResult{LocalPath: destDir, CommitSHA: sha, CloneURL: cloneURL}, nil
}

// cloneArgs are the git args for a shallow, hardened clone. Identical on the
// anonymous and authenticated paths — the token is never an argument, only an
// environment-scoped header (see hardenedGitEnv), so it can't leak via `ps`.
func cloneArgs(cloneURL, destDir string) []string {
	return []string{
		"clone",
		"--depth=1",
		"--filter=blob:none",
		"--no-tags",
		"--no-recurse-submodules",
		"--config", "core.symlinks=false",
		cloneURL,
		destDir,
	}
}

// runClone performs an anonymous shallow clone.
func runClone(ctx context.Context, cloneURL, destDir string) error {
	return execClone(ctx, cloneArgs(cloneURL, destDir), hardenedGitEnv(""))
}

// runCloneAuth performs a clone with a github.com-scoped Authorization header
// injected via git's env-config (never argv, never the URL).
func runCloneAuth(ctx context.Context, cloneURL, destDir, token string) error {
	return execClone(ctx, cloneArgs(cloneURL, destDir), hardenedGitEnv(token))
}

func execClone(ctx context.Context, args, env []string) error {
	cmd := exec.CommandContext(ctx, "git", args...) // #nosec G204 -- args constructed in-process, cloneURL built from validated segments; any token is passed via env, never argv
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone failed: %w; output: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// redactToken scrubs the token and its base64 header form from an error string,
// in case git ever echoes the injected header on failure.
func redactToken(err error, token string) error {
	if token == "" {
		return err
	}
	msg := strings.ReplaceAll(err.Error(), token, "***")
	b64 := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
	return errors.New(strings.ReplaceAll(msg, b64, "***"))
}

// hardenedGitEnv returns the environment for our subprocess. We strip
// anything that could surface a prompt or pull in user-controlled config.
//
//   - GIT_TERMINAL_PROMPT=0  — never prompt for a username/password.
//     A private repo without auth fails fast.
//   - GIT_CONFIG_GLOBAL=/dev/null — ignore the user's ~/.gitconfig
//     (which can declare hooks, credentials, etc.).
//   - GIT_CONFIG_SYSTEM=/dev/null — same for /etc/gitconfig.
//   - GIT_ASKPASS=/bin/false — second line of defense against credential prompts.
//   - HOME passed through so git can find its CA bundle on macOS; everything
//     else from the parent env is preserved (PATH for `git`, locale, etc.).
func hardenedGitEnv(token string) []string {
	env := append([]string(nil), os.Environ()...)
	env = append(env,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_ASKPASS=/bin/false",
	)
	if token != "" {
		// Inject the credential as a github.com-scoped Authorization header via
		// git's env-config (GIT_CONFIG_COUNT). This keeps the secret out of
		// argv (so it never shows in `ps`), out of the returned CloneURL, and
		// out of the cloned .git/config (env-config is not persisted). The host
		// scope means the header is only ever sent to github.com.
		auth := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
		env = append(env,
			"GIT_CONFIG_COUNT=1",
			"GIT_CONFIG_KEY_0=http.https://github.com/.extraheader",
			"GIT_CONFIG_VALUE_0=Authorization: Basic "+auth,
		)
	}
	return env
}

// readHeadSHA reads .git/HEAD (which is a ref pointer for shallow clones)
// and resolves it to the commit hash. Best-effort: returns empty string on
// any I/O failure — the caller treats that as "unknown" and proceeds.
func readHeadSHA(repoDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(repoDir, ".git", "HEAD")) // #nosec G304 -- bounded under cacheRoot
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(string(data))
	if strings.HasPrefix(line, "ref: ") {
		ref := strings.TrimPrefix(line, "ref: ")
		refPath := filepath.Join(repoDir, ".git", filepath.FromSlash(ref))
		raw, err := os.ReadFile(refPath) // #nosec G304 -- refPath built from repoDir/.git ref, bounded
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(raw)), nil
	}
	return line, nil
}

// EnsureGitInstalled reports whether `git` is on PATH. Callers use this at
// boot to decide whether to surface "GitHub fetch unavailable" in the
// connections panel. Cheap (single PATH lookup).
func EnsureGitInstalled() error {
	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("git not on PATH: install via https://git-scm.com")
	}
	return nil
}
