package api

import "net/http"

// NoCacheJSON forces fresh responses on /api/* — the local UI is the only
// consumer in v0 and stale inventory/config is confusing.
func NoCacheJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
