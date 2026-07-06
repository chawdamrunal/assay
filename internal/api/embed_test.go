package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFrontendHandlerServesIndex(t *testing.T) {
	h := NewFrontendHandler()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Result().StatusCode == http.StatusNotFound {
		t.Skip("web/dist not embedded; run `pnpm build` in web/ and `cp -r web/dist/. internal/api/dist/`")
	}
	assert.Equal(t, http.StatusOK, w.Result().StatusCode)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/html")
	assert.Contains(t, w.Body.String(), `<div id="root"`)
}

func TestFrontendHandlerSPAFallback(t *testing.T) {
	h := NewFrontendHandler()

	req := httptest.NewRequest(http.MethodGet, "/scans/some-uuid", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Result().StatusCode == http.StatusNotFound {
		t.Skip("web/dist not embedded; SPA fallback requires the build artifacts")
	}
	body := w.Body.String()
	assert.True(t, strings.Contains(body, `<div id="root"`), "SPA fallback should serve index.html for client-side routes")
}

func TestFrontendHandlerServesStaticAsset(t *testing.T) {
	h := NewFrontendHandler()

	// Use favicon.svg which exists in dist/ regardless of build hash.
	req := httptest.NewRequest(http.MethodGet, "/favicon.svg", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Result().StatusCode == http.StatusNotFound {
		t.Skip("web/dist not embedded")
	}
	assert.Equal(t, http.StatusOK, w.Result().StatusCode)
	assert.Contains(t, w.Body.String(), "<svg")
}
