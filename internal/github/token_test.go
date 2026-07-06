package github

import "testing"

func TestResolveToken(t *testing.T) {
	t.Run("keychain value wins over env", func(t *testing.T) {
		t.Setenv("GITHUB_TOKEN", "env-tok")
		tok, src := ResolveToken("kc-tok")
		if tok != "kc-tok" || src != "keychain" {
			t.Fatalf("want kc-tok/keychain, got %q/%q", tok, src)
		}
	})

	t.Run("GITHUB_TOKEN fallback when no keychain value", func(t *testing.T) {
		t.Setenv("GH_TOKEN", "")
		t.Setenv("GITHUB_TOKEN", "env-tok")
		tok, src := ResolveToken("")
		if tok != "env-tok" || src != "env:GITHUB_TOKEN" {
			t.Fatalf("want env-tok/env:GITHUB_TOKEN, got %q/%q", tok, src)
		}
	})

	t.Run("GH_TOKEN honored when GITHUB_TOKEN unset", func(t *testing.T) {
		t.Setenv("GITHUB_TOKEN", "")
		t.Setenv("GH_TOKEN", "gh-tok")
		tok, src := ResolveToken("")
		if tok != "gh-tok" || src != "env:GH_TOKEN" {
			t.Fatalf("want gh-tok/env:GH_TOKEN, got %q/%q", tok, src)
		}
	})

	t.Run("no keychain or env falls through to gh-cli or none", func(t *testing.T) {
		t.Setenv("GITHUB_TOKEN", "")
		t.Setenv("GH_TOKEN", "")
		// gh may or may not be installed/logged-in on the host — both outcomes
		// are valid; assert only that we did NOT wrongly report keychain/env.
		tok, src := ResolveToken("   ")
		if src == "keychain" || src == "env:GITHUB_TOKEN" || src == "env:GH_TOKEN" {
			t.Fatalf("unexpected source %q with no keychain/env token", src)
		}
		if (src == "none") != (tok == "") {
			t.Fatalf("token/source mismatch: %q/%q", tok, src)
		}
	})
}
