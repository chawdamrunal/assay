package api

import (
	"context"
	"net/http"

	"github.com/chawdamrunal/assay/internal/assistant"
)

// Deps holds the dependencies the API handlers need.
// The Frontend handler is the embedded React SPA — anything not under
// /api/* or /healthz falls through to it (SPA needs to handle client-side
// routes like /inventory, /scans/abc, etc.).
type Deps struct {
	LoadInventory InventoryLoader
	ConfigPath    string
	ScansDir      string
	FleetDir      string // ~/.assay/fleet — set in v0.4 for fleet routes; empty disables them
	// MarketplacesDir is the chat-assistant's secondary plugin source — when
	// empty, the assistant only resolves against installed inventory.
	MarketplacesDir string
	// GitHubCacheDir is the directory chat-initiated GitHub clones land in
	// (typically ~/.assay/sources/). When empty, GitHub auto-fetch is
	// disabled and the assistant falls back to telling the user to clone
	// manually.
	GitHubCacheDir string
	// KeychainService is the OS keychain service name provider API keys are
	// stored under (production: "assay"). Drives the /api/keys endpoints and the
	// per-provider rows in /api/status.
	KeychainService string
	Scans           ScansDeps  // if zero, falls back to placeholder string-based handler
	Status         StatusDeps // /api/status probe inputs; zero value disables the route
	// Assistant is allocated lazily inside NewHandler so the chat conversation
	// state persists for the lifetime of the serve process.
	AssistantStore *assistant.ConversationStore
	Frontend       http.Handler
	// ServerCtx is the server-lifetime context, canceled on shutdown/SIGTERM.
	// Propagated into ScansDeps + FleetDeps so background scan goroutines are
	// canceled cleanly instead of orphaned. Nil → background (tests).
	ServerCtx context.Context
}

// NewHandler builds the http.Handler tree. Every mutation route (POST /
// PUT / DELETE / PATCH) is wrapped in CSRFRequired so cross-origin pages
// can't trigger scans or deletes via the user's localhost browser.
func NewHandler(d Deps) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/api/inventory", NoCacheJSON(NewInventoryHandler(d.LoadInventory)))
	// Supply-chain summary feeds the Dashboard tile and is cheap (walks
	// scans dir on demand). Read-only, no CSRF needed.
	mux.Handle("/api/supply-chain/summary", NoCacheJSON(NewSupplyChainHandler(d.ScansDir)))
	// PUT /api/config mutates state; CSRF guard blocks bare cross-origin PUT.
	// GET still passes straight through.
	mux.Handle("/api/config", NoCacheJSON(CSRFRequired(NewConfigHandler(d.ConfigPath))))
	mux.Handle("/api/status", NoCacheJSON(NewStatusHandler(d.Status)))
	// Provider API keys. POST is a write-only mutation (key → OS keychain,
	// never echoed) so it is CSRF-guarded; the status GET returns booleans only.
	mux.Handle("/api/keys", NoCacheJSON(CSRFRequired(NewKeysHandler(KeysDeps{KeychainService: d.KeychainService}))))
	mux.Handle("/api/keys/status", NoCacheJSON(NewKeyStatusHandler(KeysDeps{KeychainService: d.KeychainService})))
	// GitHub PAT for private-repo cloning. POST/DELETE mutate the keychain, so
	// CSRF-guarded and write-only — the token is never echoed; its presence +
	// source surface via /api/status.
	mux.Handle("/api/github-token", NoCacheJSON(CSRFRequired(NewGitHubTokenHandler(KeysDeps{KeychainService: d.KeychainService}))))

	// Propagate the server-lifetime context so background scan goroutines are
	// canceled on shutdown (set once here rather than at every call site).
	d.Scans.ServerCtx = d.ServerCtx

	var scansHandler http.Handler
	if d.Scans.Runner != nil {
		scansHandler = NewScansHandler(d.Scans)
	} else {
		scansHandler = NewScansHandler(d.ScansDir)
	}
	// `/api/scans/diff` is registered as an exact path so it wins over the
	// `/api/scans/` prefix that delegates to the scan-id router. Without
	// this, GET /api/scans/diff?a=...&b=... gets parsed as scan_id="diff".
	mux.Handle("/api/scans/diff", NoCacheJSON(NewDiffHandler(d.ScansDir)))
	// Wrap scans in CSRFRequired so POST /api/scans and DELETE /api/scans/:id
	// can't be triggered cross-origin. GET routes pass through unchanged.
	mux.Handle("/api/scans", NoCacheJSON(CSRFRequired(scansHandler)))
	mux.Handle("/api/scans/", NoCacheJSON(CSRFRequired(scansHandler)))

	// Fleet routes — only when FleetDir is provided (cmd_serve fills it in
	// when it has a paths.DataDir). Otherwise the route is unregistered and
	// the FE falls back gracefully (no Fleet nav entry shown).
	if d.FleetDir != "" && d.Scans.Runner != nil {
		fleetHandler := NewFleetHandler(FleetDeps{
			FleetDir:      d.FleetDir,
			ScansDir:      d.ScansDir,
			LoadInventory: d.LoadInventory,
			StartScan:     d.Scans.StartScan,
			Runner:        d.Scans.Runner,
			ServerCtx:     d.ServerCtx,
		})
		// POST /api/fleet/scan is a mutation; CSRFRequired blocks it from
		// cross-origin pages. GET fleet list/detail/stream pass through.
		mux.Handle("/api/fleet", NoCacheJSON(CSRFRequired(fleetHandler)))
		mux.Handle("/api/fleet/", NoCacheJSON(CSRFRequired(fleetHandler)))
	}

	// Assistant routes — only when StartScan is wired (otherwise the
	// confirm action would silently no-op). Store is allocated here if the
	// caller didn't pass one, so each serve process gets a fresh in-memory
	// conversation map.
	if d.Scans.StartScan != nil && d.Scans.Runner != nil {
		store := d.AssistantStore
		if store == nil {
			store = assistant.NewConversationStore()
		}
		assistantHandler := NewAssistantHandler(AssistantDeps{
			LoadInventory:   d.LoadInventory,
			MarketplacesDir: d.MarketplacesDir,
			Store:           store,
			StartScan:       d.Scans.StartScan,
			Runner:          d.Scans.Runner,
			AllowedRoots:    d.Scans.AllowedRoots,
			GitHubCacheDir:  d.GitHubCacheDir,
			GitHubToken:     githubTokenFunc(d.KeychainService),
		})
		// POST-only endpoint; CSRFRequired blocks cross-origin chat-initiated
		// scans which would otherwise be possible from any page the user has
		// open in another tab.
		mux.Handle("/api/assistant/message", NoCacheJSON(CSRFRequired(assistantHandler)))
	}

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("/", d.Frontend)
	return mux
}
