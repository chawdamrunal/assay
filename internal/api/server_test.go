package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chawdamrunal/assay/internal/inventory"
	"github.com/chawdamrunal/assay/internal/store"
)

func TestServerRoutesInventoryConfigScansHealthz(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.toml")
	require.NoError(t, store.SaveConfig(configPath, store.DefaultConfig()))

	loader := func() (inventory.Inventory, error) {
		return inventory.Inventory{}, nil
	}

	h := NewHandler(Deps{
		LoadInventory: loader,
		ConfigPath:    configPath,
		ScansDir:      filepath.Join(tmp, "scans"),
		Frontend:      http.NotFoundHandler(),
	})

	cases := []struct {
		path string
		code int
	}{
		{"/api/inventory", http.StatusOK},
		{"/api/config", http.StatusOK},
		{"/api/scans", http.StatusOK},
		{"/api/scans/unknown", http.StatusNotFound},
		{"/healthz", http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			assert.Equal(t, tc.code, w.Result().StatusCode)
		})
	}
}

func TestServerFallsBackToFrontendOnUnknownPath(t *testing.T) {
	fe := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("frontend"))
	})
	h := NewHandler(Deps{
		LoadInventory: func() (inventory.Inventory, error) { return inventory.Inventory{}, nil },
		ConfigPath:    filepath.Join(t.TempDir(), "c.toml"),
		ScansDir:      t.TempDir(),
		Frontend:      fe,
	})

	req := httptest.NewRequest(http.MethodGet, "/inventory", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Result().StatusCode)
	assert.Equal(t, "frontend", w.Body.String())
}
