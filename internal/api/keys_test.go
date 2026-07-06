package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"

	"github.com/chawdamrunal/assay/internal/store"
)

func postKey(t *testing.T, h http.Handler, provider, key string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"provider": provider, "key": key})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/keys", bytes.NewReader(body)))
	return w
}

func TestSetKeyHandler(t *testing.T) {
	keyring.MockInit()
	h := NewKeysHandler(KeysDeps{KeychainService: "assay-test"})

	// Valid POST → 204, key stored, never echoed.
	w := postKey(t, h, "gemini-api", "AIza-secret-value")
	require.Equal(t, http.StatusNoContent, w.Result().StatusCode)
	assert.NotContains(t, w.Body.String(), "AIza-secret-value", "key must never be echoed back")
	stored, err := store.NewKeyring("assay-test").GetProviderKey("gemini-api")
	require.NoError(t, err)
	assert.Equal(t, "AIza-secret-value", stored)

	// Unknown provider → 400.
	assert.Equal(t, http.StatusBadRequest, postKey(t, h, "bogus", "x").Result().StatusCode)
	// claude-code has no API key concept → 400.
	assert.Equal(t, http.StatusBadRequest, postKey(t, h, "claude-code", "x").Result().StatusCode)
	// Empty key → 400.
	assert.Equal(t, http.StatusBadRequest, postKey(t, h, "openai-api", "").Result().StatusCode)

	// Wrong method → 405.
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/keys", nil))
	assert.Equal(t, http.StatusMethodNotAllowed, w.Result().StatusCode)
}

func TestKeyStatusHandler(t *testing.T) {
	keyring.MockInit()
	require.NoError(t, store.NewKeyring("assay-test").SetProviderKey("gemini-api", "AIza-x"))

	h := NewKeyStatusHandler(KeysDeps{KeychainService: "assay-test"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/keys/status", nil))
	require.Equal(t, http.StatusOK, w.Result().StatusCode)

	var resp struct {
		Providers map[string]bool `json:"providers"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.Providers["gemini-api"], "gemini-api should report configured")
	assert.False(t, resp.Providers["openai-api"], "openai-api should report not-configured")
	assert.NotContains(t, w.Body.String(), "AIza-x", "status must never expose key values")
}
