package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCSRFRequiredRejectsMissingHeaderOnPOST regression-guards the v0.5.1
// CSRF fix. The middleware MUST return 403 when a POST request omits the
// X-Assay-CSRF header — that's the gate that prevents a cross-origin page
// from triggering scans via the user's localhost browser.
func TestCSRFRequiredRejectsMissingHeaderOnPOST(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	})
	h := CSRFRequired(inner)

	req := httptest.NewRequest(http.MethodPost, "/api/anything", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	require.Equal(t, http.StatusForbidden, w.Result().StatusCode)
	assert.False(t, called, "inner handler must not run when CSRF header is missing")
	assert.Contains(t, w.Body.String(), CSRFHeaderName)
}

// TestCSRFRequiredAcceptsWithHeader confirms the gate lets the request
// through when the header is present.
func TestCSRFRequiredAcceptsWithHeader(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	})
	h := CSRFRequired(inner)

	req := httptest.NewRequest(http.MethodPost, "/api/anything", strings.NewReader(`{}`))
	req.Header.Set(CSRFHeaderName, CSRFHeaderValue)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.True(t, called, "inner handler should run when CSRF header is set")
}

// TestCSRFRequiredSkipsSafeMethods documents the design: GET / HEAD / OPTIONS
// pass through without the header because they cannot mutate state.
func TestCSRFRequiredSkipsSafeMethods(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		t.Run(method, func(t *testing.T) {
			called := false
			inner := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
				called = true
			})
			h := CSRFRequired(inner)
			req := httptest.NewRequest(method, "/api/anything", nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			assert.True(t, called, "%s must pass through without CSRF header", method)
		})
	}
}

// TestCSRFRequiredRejectsDELETEAndPUT confirms the gate applies to every
// mutation verb (not just POST).
func TestCSRFRequiredRejectsDELETEAndPUT(t *testing.T) {
	for _, method := range []string{http.MethodDelete, http.MethodPut, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			h := CSRFRequired(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
			req := httptest.NewRequest(method, "/api/anything", nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			assert.Equal(t, http.StatusForbidden, w.Result().StatusCode)
		})
	}
}
