package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/chawdamrunal/assay/internal/github"
	"github.com/chawdamrunal/assay/internal/store"
)

// NewGitHubTokenHandler serves POST/DELETE /api/github-token — write-only
// storage of a GitHub personal access token (for cloning private repos) into
// the OS keychain. The token is NEVER returned by any endpoint; its presence
// and source are reported (labels only) via /api/status. Mirrors the
// write-only contract of NewKeysHandler.
func NewGitHubTokenHandler(deps KeysDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if deps.KeychainService == "" {
			WriteJSONError(w, http.StatusInternalServerError, "keychain service not configured")
			return
		}
		kr := store.NewKeyring(deps.KeychainService)
		switch r.Method {
		case http.MethodPost:
			var req struct {
				Token string `json:"token"`
			}
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
				WriteJSONError(w, http.StatusBadRequest, "invalid JSON body")
				return
			}
			token := strings.TrimSpace(req.Token)
			if token == "" {
				WriteJSONError(w, http.StatusBadRequest, "token cannot be empty")
				return
			}
			if err := kr.SetGitHubToken(token); err != nil {
				// The OS keychain can be unavailable — headless/CI, a locked or
				// denied macOS Keychain, or a Linux box with no Secret Service.
				// Point the user at the keychain-free path instead of dead-ending.
				WriteJSONError(w, http.StatusInternalServerError,
					"could not store the token in the OS keychain ("+err.Error()+"). "+
						"If the keychain is unavailable (headless, CI, or no secret service), "+
						"skip this field and set the GITHUB_TOKEN env var or run `gh auth login` "+
						"instead — Assay auto-detects both, no keychain required.")
				return
			}
			w.WriteHeader(http.StatusNoContent) // token is never echoed back
		case http.MethodDelete:
			if err := kr.DeleteGitHubToken(); err != nil {
				WriteJSONError(w, http.StatusInternalServerError, "could not delete token: "+err.Error())
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			WriteJSONError(w, http.StatusMethodNotAllowed, "POST or DELETE /api/github-token")
		}
	})
}

// githubTokenFunc builds the resolver Clone uses to reach private repos: an
// explicitly-stored keychain token takes precedence, then the GITHUB_TOKEN /
// GH_TOKEN env vars, then `gh auth token` — via github.ResolveToken. The
// returned func is always non-nil; it yields ("", "none") when nothing is
// available, which Clone treats as an anonymous (public-only) clone.
func githubTokenFunc(keychainService string) github.TokenFunc {
	return func() (string, string) {
		var kcToken string
		if keychainService != "" {
			if t, err := store.NewKeyring(keychainService).GetGitHubToken(); err == nil {
				kcToken = t
			}
		}
		return github.ResolveToken(kcToken)
	}
}
