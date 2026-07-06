package api

import (
	"encoding/json"
	"net/http"

	"github.com/chawdamrunal/assay/internal/provider"
	"github.com/chawdamrunal/assay/internal/store"
)

// KeysDeps are the dependencies for the API-key endpoints.
type KeysDeps struct {
	// KeychainService is the OS keychain service name keys are stored under
	// (production: "assay"). Empty disables writes.
	KeychainService string
}

type setKeyRequest struct {
	Provider string `json:"provider"`
	Key      string `json:"key"`
}

// NewKeysHandler serves POST /api/keys — write-only storage of a direct-API
// provider key into the OS keychain. The key value is NEVER returned by any
// endpoint; the response is 204 on success. Pair with NewKeyStatusHandler.
func NewKeysHandler(deps KeysDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			WriteJSONError(w, http.StatusMethodNotAllowed, "POST /api/keys to set a provider API key")
			return
		}
		var req setKeyRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
			WriteJSONError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		id := provider.AgentID(req.Provider)
		if !id.Known() || !id.IsAPI() {
			WriteJSONError(w, http.StatusBadRequest,
				"provider must be a direct-API provider (anthropic-api, gemini-api, openai-api)")
			return
		}
		if req.Key == "" {
			WriteJSONError(w, http.StatusBadRequest, "key cannot be empty")
			return
		}
		if deps.KeychainService == "" {
			WriteJSONError(w, http.StatusInternalServerError, "keychain service not configured")
			return
		}
		if err := store.NewKeyring(deps.KeychainService).SetProviderKey(req.Provider, req.Key); err != nil {
			WriteJSONError(w, http.StatusInternalServerError, "could not store key: "+err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent) // key is never echoed back
	})
}

// NewKeyStatusHandler serves GET /api/keys/status →
// {"providers":{"anthropic-api":true,"gemini-api":false,"openai-api":false}}.
// Booleans only — key values are never exposed. Read-only (no CSRF).
func NewKeyStatusHandler(deps KeysDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			WriteJSONError(w, http.StatusMethodNotAllowed, "GET /api/keys/status")
			return
		}
		out := map[string]bool{}
		if deps.KeychainService != "" {
			kr := store.NewKeyring(deps.KeychainService)
			for _, id := range provider.APIProviders() {
				out[string(id)] = kr.HasProviderKey(string(id))
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"providers": out})
	})
}
