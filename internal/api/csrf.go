package api

import "net/http"

// CSRFHeaderName is the request header the middleware requires on every
// non-safe (non-GET / non-HEAD / non-OPTIONS) request.
const CSRFHeaderName = "X-Assay-CSRF"

// CSRFHeaderValue is the literal value the FE sends. Validating against a
// constant is sufficient because the goal of this check is NOT to prevent
// authenticated request forgery (we have no auth) — it's to prevent a
// cross-origin page from triggering scans / deletions through the user's
// localhost browser.
//
// How the protection works: a cross-origin page can issue a "simple" fetch
// to http://localhost:7373/api/scans without a preflight. But adding any
// custom header (X-Assay-CSRF) downgrades the request from "simple" to
// "CORS-preflighted". The browser then sends an OPTIONS preflight, sees
// our server has no Access-Control-Allow-Origin response, and refuses to
// send the real request. So the missing-header rejection is the gate that
// makes the protection work — the value itself is incidental.
const CSRFHeaderValue = "1"

// CSRFRequired wraps an http.Handler so that every state-mutating request
// (anything that isn't GET / HEAD / OPTIONS) must carry the CSRF header.
// Requests missing the header are rejected with 403.
//
// Safe methods are passed through unchanged because they cannot mutate
// server state (by HTTP spec). A malicious cross-origin GET would expose
// at most the response body to the attacker page — but the same-origin
// policy already blocks the attacker from reading it without our
// cooperation (no CORS headers in the response).
func CSRFRequired(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if r.Header.Get(CSRFHeaderName) == "" {
			WriteJSONError(w, http.StatusForbidden,
				"missing "+CSRFHeaderName+" header; cross-origin requests must include it")
			return
		}
		next.ServeHTTP(w, r)
	})
}
