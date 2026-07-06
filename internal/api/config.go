package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/chawdamrunal/assay/internal/store"
)

// validateConfig rejects semantically invalid config a PUT could otherwise
// silently persist. A negative budget or concurrency is always wrong; an
// absurd concurrency would fan out unbounded subprocesses. BudgetUSD == 0 is
// allowed and documented as "no cap" — correct for the subscription/MCP model
// where there is no per-token dollar cost.
func validateConfig(cfg store.Config) error {
	if cfg.Scan.BudgetUSD < 0 {
		return fmt.Errorf("scan.budget_usd must be >= 0 (0 means no cap); got %.2f", cfg.Scan.BudgetUSD)
	}
	if cfg.Scan.SubagentConcurrency < 0 || cfg.Scan.SubagentConcurrency > 64 {
		return fmt.Errorf("scan.subagent_concurrency must be between 0 and 64; got %d", cfg.Scan.SubagentConcurrency)
	}
	return nil
}

// NewConfigHandler returns a handler for /api/config:
//
//	GET  -> current Config as JSON
//	PUT  -> replace Config (rejects telemetry.enabled = true; v0 forces off)
func NewConfigHandler(configPath string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			cfg, err := store.LoadConfig(configPath)
			if err != nil {
				WriteJSONError(w, http.StatusInternalServerError, err.Error())
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(cfg)
		case http.MethodPut:
			var cfg store.Config
			if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
				WriteJSONError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
				return
			}
			if cfg.Telemetry.Enabled {
				WriteJSONError(w, http.StatusBadRequest, "telemetry is forced off in v0")
				return
			}
			if err := validateConfig(cfg); err != nil {
				WriteJSONError(w, http.StatusBadRequest, err.Error())
				return
			}
			if err := store.SaveConfig(configPath, cfg); err != nil {
				WriteJSONError(w, http.StatusInternalServerError, err.Error())
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(cfg)
		default:
			WriteJSONError(w, http.StatusMethodNotAllowed, "only GET and PUT supported")
		}
	})
}
