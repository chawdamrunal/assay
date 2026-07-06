// Package api implements the local HTTP server that the Assay CLI's
// `assay serve` command exposes. The same handlers are consumed by the
// embedded React SPA and by anyone scripting against the local API.
package api

import (
	"encoding/json"
	"net/http"
)

// WriteJSONError emits a small problem-details-ish JSON body with the
// given HTTP status code and human-readable message.
func WriteJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status": http.StatusText(status),
		"error":  message,
	})
}
