package api_test

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chawdamrunal/assay/internal/api"
	"github.com/chawdamrunal/assay/internal/inventory"
	"github.com/chawdamrunal/assay/internal/store"
)

func TestEndToEndAPI(t *testing.T) {
	// Fake Claude dir
	claudeDir := t.TempDir()
	pluginDir := filepath.Join(claudeDir, "plugins", "rainbow")
	require.NoError(t, os.MkdirAll(pluginDir, 0o750))
	require.NoError(t, os.WriteFile(
		filepath.Join(pluginDir, "plugin.json"),
		[]byte(`{"name":"rainbow","version":"1.0.0"}`), 0o600))

	// Assay config path
	dataDir := t.TempDir()
	configPath := filepath.Join(dataDir, "config.toml")
	require.NoError(t, store.SaveConfig(configPath, store.DefaultConfig()))

	loader := func() (inventory.Inventory, error) {
		return inventory.EnumerateAll(inventory.EnumerateOptions{
			PluginsDir:   filepath.Join(claudeDir, "plugins"),
			SettingsFile: filepath.Join(claudeDir, "settings.json"),
		})
	}

	handler := api.NewHandler(api.Deps{
		LoadInventory: loader,
		ConfigPath:    configPath,
		ScansDir:      filepath.Join(dataDir, "scans"),
		Frontend:      http.NotFoundHandler(),
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	url := "http://" + ln.Addr().String()

	// healthz
	resp, err := http.Get(url + "/healthz")
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	assert.Equal(t, "ok", string(body))

	// inventory
	resp, err = http.Get(url + "/api/inventory")
	require.NoError(t, err)
	var inv inventory.Inventory
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&inv))
	_ = resp.Body.Close()
	require.Len(t, inv.Items, 1)
	assert.Equal(t, "rainbow", inv.Items[0].Name)

	// config
	resp, err = http.Get(url + "/api/config")
	require.NoError(t, err)
	var cfg store.Config
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&cfg))
	_ = resp.Body.Close()
	assert.Equal(t, store.DefaultConfig(), cfg)

	// scans list
	resp, err = http.Get(url + "/api/scans")
	require.NoError(t, err)
	scansBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	assert.JSONEq(t, `{"items":[]}`, string(scansBody))

	// scans POST without CSRF header → 403 (added in v0.5.1)
	noCSRF, err := http.Post(url+"/api/scans", "application/json", nil)
	require.NoError(t, err)
	_ = noCSRF.Body.Close()
	assert.Equal(t, http.StatusForbidden, noCSRF.StatusCode,
		"requests missing X-Assay-CSRF must be rejected with 403")

	// Same POST with the CSRF header set → 501 (placeholder; real scan path
	// requires ScansDeps wiring, which this test's bare-deps server lacks).
	req, _ := http.NewRequest(http.MethodPost, url+"/api/scans", nil)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(api.CSRFHeaderName, api.CSRFHeaderValue)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusNotImplemented, resp.StatusCode)
}
