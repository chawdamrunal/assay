package github

import (
	"encoding/base64"
	"slices"
	"strings"
	"testing"
)

// The core security invariant: an injected token appears only as a
// github.com-scoped, base64 Authorization header in the child ENV — never as
// the raw value, and never in argv.
func TestHardenedGitEnv_TokenScopedNotLeaked(t *testing.T) {
	const token = "ghp_SUPERSECRET_value_1234567890"
	env := hardenedGitEnv(token)

	if strings.Contains(strings.Join(env, "\n"), token) {
		t.Fatal("raw token leaked verbatim into the git env")
	}
	if !slices.Contains(env, "GIT_CONFIG_COUNT=1") ||
		!slices.Contains(env, "GIT_CONFIG_KEY_0=http.https://github.com/.extraheader") {
		t.Fatalf("missing github.com-scoped extraheader config; env=%v", env)
	}
	b64 := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
	if !slices.Contains(env, "GIT_CONFIG_VALUE_0=Authorization: Basic "+b64) {
		t.Fatal("auth header value missing or malformed")
	}
	if !slices.Contains(env, "GIT_TERMINAL_PROMPT=0") {
		t.Fatal("terminal prompt must stay disabled even with a token")
	}
}

func TestHardenedGitEnv_AnonInjectsNoHeader(t *testing.T) {
	for _, e := range hardenedGitEnv("") {
		if strings.HasPrefix(e, "GIT_CONFIG_COUNT") || strings.Contains(e, "extraheader") {
			t.Fatalf("anonymous clone must inject no auth header, got %q", e)
		}
	}
}

// cloneArgs must never carry the credential — auth travels only via env, so the
// token can't appear in `ps`/argv or persist in .git/config.
func TestCloneArgs_CarryNoCredential(t *testing.T) {
	for _, a := range cloneArgs("https://github.com/o/r.git", "/tmp/dest") {
		if strings.Contains(a, "Authorization") || strings.Contains(a, "extraheader") || strings.Contains(a, "@github") {
			t.Fatalf("clone args must not carry credentials, got %q", a)
		}
	}
}

func TestRedactToken(t *testing.T) {
	const token = "ghp_leak_me_if_you_can"
	b64 := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
	err := redactToken(errWrap("fatal: bad header "+token+" and "+b64), token)
	if strings.Contains(err.Error(), token) || strings.Contains(err.Error(), b64) {
		t.Fatalf("token not fully redacted: %v", err)
	}
}

func errWrap(s string) error { return &stringErr{s} }

type stringErr struct{ s string }

func (e *stringErr) Error() string { return e.s }
