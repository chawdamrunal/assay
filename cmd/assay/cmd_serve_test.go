package main

import (
	"io"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chawdamrunal/assay/internal/store"
)

// TestRunServeServesHealthz starts runServe on a random localhost port,
// hits /healthz, and confirms the listener responds before tearing down.
func TestRunServeServesHealthz(t *testing.T) {
	tmp := t.TempDir()
	paths := &store.Paths{
		ConfigDir:  tmp,
		ConfigFile: filepath.Join(tmp, "config.toml"),
		DataDir:    tmp,
		ScansDir:   filepath.Join(tmp, "scans"),
		CacheDir:   filepath.Join(tmp, "cache"),
	}
	require.NoError(t, paths.Ensure())
	require.NoError(t, store.SaveConfig(paths.ConfigFile, store.DefaultConfig()))

	// Bind to a random port by passing :0; runServe writes the actual addr
	// to the returned channel after the listener is up.
	ready := make(chan string, 1)
	stop := make(chan struct{})
	errCh := make(chan error, 1)

	go func() {
		errCh <- runServe("127.0.0.1:0", http.NotFoundHandler(), ready, stop, paths, t.TempDir(), "legacy", "", "")
	}()

	var addr string
	select {
	case addr = <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("runServe did not signal ready within 2s")
	}

	resp, err := http.Get("http://" + addr + "/healthz")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "ok", string(body))

	close(stop)
	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("runServe did not exit within 2s after stop signal")
	}
}

func TestRunServeInvalidBindReturnsError(t *testing.T) {
	tmp := t.TempDir()
	paths := &store.Paths{
		ConfigDir:  tmp,
		ConfigFile: filepath.Join(tmp, "config.toml"),
		DataDir:    tmp,
		ScansDir:   filepath.Join(tmp, "scans"),
		CacheDir:   filepath.Join(tmp, "cache"),
	}
	require.NoError(t, paths.Ensure())

	ready := make(chan string, 1)
	stop := make(chan struct{})

	err := runServe("definitely:not:a:valid:bind", http.NotFoundHandler(), ready, stop, paths, "", "legacy", "", "")
	require.Error(t, err)
	// Make sure we don't leak goroutines: stop is unused, that's fine
	_ = net.JoinHostPort // keeps the net import alive for future expansion
}
