package sca

import (
	"testing"

	"github.com/chawdamrunal/assay/internal/prepass"
)

// TestToFindingUniqueIDAndEvidenceFile guards the two SCA fixes: finding IDs
// must include package@version so two packages hit by the SAME advisory don't
// collide (which would break policy suppression and diff-mode keying), and the
// evidence file must be the real source manifest, not a hardcoded
// package-lock.json (so Go/Rust/Python findings cite the right file).
func TestToFindingUniqueIDAndEvidenceFile(t *testing.T) {
	h := prepass.Hit{Severity: "high", Message: "vuln", Metadata: map[string]string{"id": "CVE-2024-0001"}}
	a := toFinding(Coord{Ecosystem: "npm", Name: "lodash", Version: "4.17.4", SourceFile: "package-lock.json"}, h)
	b := toFinding(Coord{Ecosystem: "Go", Name: "lodash", Version: "4.17.20", SourceFile: "go.sum"}, h)

	if a.ID == b.ID {
		t.Fatalf("two versions sharing one CVE must get distinct IDs; both were %q", a.ID)
	}
	if len(a.Evidence) == 0 || a.Evidence[0].File != "package-lock.json" {
		t.Fatalf("npm finding should cite package-lock.json, got %+v", a.Evidence)
	}
	if len(b.Evidence) == 0 || b.Evidence[0].File != "go.sum" {
		t.Fatalf("Go finding should cite go.sum, not package-lock.json, got %+v", b.Evidence)
	}
}
