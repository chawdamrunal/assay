package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chawdamrunal/assay/internal/store"
)

func TestConfigHandlerGet(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.toml")
	require.NoError(t, store.SaveConfig(path, store.DefaultConfig()))

	h := NewConfigHandler(path)
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var cfg store.Config
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&cfg))
	assert.Equal(t, store.DefaultConfig(), cfg)
}

func TestConfigHandlerPut(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.toml")

	newCfg := store.DefaultConfig()
	newCfg.Scan.BudgetUSD = 17.5
	body, err := json.Marshal(newCfg)
	require.NoError(t, err)

	h := NewConfigHandler(path)
	req := httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	got, err := store.LoadConfig(path)
	require.NoError(t, err)
	assert.Equal(t, 17.5, got.Scan.BudgetUSD)
}

func TestConfigHandlerRejectsTelemetryEnable(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.toml")

	bad := store.DefaultConfig()
	bad.Telemetry.Enabled = true
	body, err := json.Marshal(bad)
	require.NoError(t, err)

	h := NewConfigHandler(path)
	req := httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
}

func TestConfigHandlerRejectsBadMethod(t *testing.T) {
	h := NewConfigHandler(filepath.Join(t.TempDir(), "c.toml"))
	req := httptest.NewRequest(http.MethodDelete, "/api/config", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Result().StatusCode)
}
