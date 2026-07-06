package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chawdamrunal/assay/internal/inventory"
)

func TestInventoryHandlerSuccess(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "plugins", "rainbow")
	require.NoError(t, os.MkdirAll(pluginDir, 0o750))
	require.NoError(t, os.WriteFile(
		filepath.Join(pluginDir, "plugin.json"),
		[]byte(`{"name":"rainbow","version":"1.0.0"}`), 0o600))

	loader := func() (inventory.Inventory, error) {
		return inventory.EnumerateAll(inventory.EnumerateOptions{
			PluginsDir: filepath.Join(root, "plugins"),
		})
	}
	h := NewInventoryHandler(loader)

	req := httptest.NewRequest(http.MethodGet, "/api/inventory", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var inv inventory.Inventory
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&inv))
	require.Len(t, inv.Items, 1)
	assert.Equal(t, "rainbow", inv.Items[0].Name)
}

func TestInventoryHandlerError(t *testing.T) {
	loader := func() (inventory.Inventory, error) {
		return inventory.Inventory{}, errors.New("boom")
	}
	h := NewInventoryHandler(loader)

	req := httptest.NewRequest(http.MethodGet, "/api/inventory", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Result().StatusCode)
}

func TestInventoryHandlerRejectsNonGET(t *testing.T) {
	loader := func() (inventory.Inventory, error) {
		return inventory.Inventory{}, nil
	}
	h := NewInventoryHandler(loader)

	req := httptest.NewRequest(http.MethodPost, "/api/inventory", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Result().StatusCode)
}
