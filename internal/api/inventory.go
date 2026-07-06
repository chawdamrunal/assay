package api

import (
	"encoding/json"
	"net/http"

	"github.com/chawdamrunal/assay/internal/inventory"
)

// InventoryLoader fetches the current inventory. The handler closes over
// one of these so callers (server.go) can inject test or production wiring.
type InventoryLoader func() (inventory.Inventory, error)

// NewInventoryHandler returns a handler for GET /api/inventory.
func NewInventoryHandler(load InventoryLoader) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			WriteJSONError(w, http.StatusMethodNotAllowed, "only GET supported")
			return
		}
		inv, err := load()
		if err != nil {
			WriteJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(inv)
	})
}
