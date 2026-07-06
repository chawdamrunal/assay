package github

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ResolveToken returns a GitHub token and a short source label, checking in
// priority order: an explicitly-configured token (the keychain value the caller
// passes in), the GITHUB_TOKEN / GH_TOKEN env vars, then `gh auth token`. The
// gh CLI call is bounded to 3s. Returns ("", "none") when nothing is available.
func ResolveToken(keychainToken string) (token, source string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return ResolveTokenContext(ctx, keychainToken)
}

// ResolveTokenContext is ResolveToken with a caller-supplied deadline for the
// `gh auth token` sub-call, so latency-sensitive callers (the status probe)
// can keep it tight while the clone path allows longer. The keychain value is
// passed in rather than read here so this package stays decoupled from
// internal/store.
func ResolveTokenContext(ctx context.Context, keychainToken string) (token, source string) {
	if t := strings.TrimSpace(keychainToken); t != "" {
		return t, "keychain"
	}
	for _, env := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if t := strings.TrimSpace(os.Getenv(env)); t != "" {
			return t, "env:" + env
		}
	}
	if t := ghCLIToken(ctx); t != "" {
		return t, "gh-cli"
	}
	return "", "none"
}

// ghCLIToken runs `gh auth token` when the GitHub CLI is on PATH. Best-effort:
// any failure (gh missing, not logged in, ctx deadline) yields "".
func ghCLIToken(ctx context.Context) string {
	if _, err := exec.LookPath("gh"); err != nil {
		return ""
	}
	out, err := exec.CommandContext(ctx, "gh", "auth", "token").Output() // #nosec G204 -- fixed args; gh looked up on PATH
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
