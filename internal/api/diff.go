package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"

	assaymcp "github.com/chawdamrunal/assay/internal/mcp"
	"github.com/chawdamrunal/assay/internal/verdict"
)

// DiffResponse is the body returned by GET /api/scans/diff. The frontend
// renders a side-by-side view; for compactness we include each verdict in
// full plus the bucketed diff lists.
type DiffResponse struct {
	A        *verdict.Verdict  `json:"a"`
	B        *verdict.Verdict  `json:"b"`
	Added    []verdict.Finding `json:"added"`    // present in B only
	Changed  []verdict.Finding `json:"changed"`  // in both, severity/evidence/description drift
	Stable   []verdict.Finding `json:"stable"`   // in both, no material change
	Resolved []verdict.Finding `json:"resolved"` // present in A only
}

// NewDiffHandler returns the /api/scans/diff handler. scansDir is the root
// under which scan_id lookups resolve.
func NewDiffHandler(scansDir string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			WriteJSONError(w, http.StatusMethodNotAllowed, "diff requires GET")
			return
		}
		aID := r.URL.Query().Get("a")
		bID := r.URL.Query().Get("b")
		if aID == "" || bID == "" {
			WriteJSONError(w, http.StatusBadRequest, "a and b query params required")
			return
		}
		if !safeScanID(aID) || !safeScanID(bID) {
			WriteJSONError(w, http.StatusBadRequest, "invalid scan id")
			return
		}
		aV, err := loadVerdict(scansDir, aID)
		if err != nil {
			writeLoadErr(w, "a", aID, err)
			return
		}
		bV, err := loadVerdict(scansDir, bID)
		if err != nil {
			writeLoadErr(w, "b", bID, err)
			return
		}
		annotated, resolved := verdict.Diff(aV.Findings, bV.Findings)
		resp := DiffResponse{
			A:        aV,
			B:        bV,
			Resolved: resolved,
		}
		for _, f := range annotated {
			if f.Diff == nil {
				continue
			}
			switch f.Diff.Status {
			case "new":
				resp.Added = append(resp.Added, f)
			case "changed":
				resp.Changed = append(resp.Changed, f)
			case "stable":
				resp.Stable = append(resp.Stable, f)
			}
		}
		// Ensure empty arrays serialize as [] not null.
		if resp.Added == nil {
			resp.Added = []verdict.Finding{}
		}
		if resp.Changed == nil {
			resp.Changed = []verdict.Finding{}
		}
		if resp.Stable == nil {
			resp.Stable = []verdict.Finding{}
		}
		if resp.Resolved == nil {
			resp.Resolved = []verdict.Finding{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
}

func loadVerdict(scansDir, scanID string) (*verdict.Verdict, error) {
	dir, err := assaymcp.FindScanDir(scansDir, scanID)
	if err != nil {
		return nil, err
	}
	return assaymcp.LoadVerdictFromDir(dir)
}

func writeLoadErr(w http.ResponseWriter, label, scanID string, err error) {
	if errors.Is(err, os.ErrNotExist) {
		WriteJSONError(w, http.StatusNotFound, label+" scan not found: "+scanID)
		return
	}
	WriteJSONError(w, http.StatusInternalServerError, label+": "+err.Error())
}
