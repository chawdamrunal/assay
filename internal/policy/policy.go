// Package policy implements Assay's policy-as-code layer: a .assay-policy.json
// file that lets a team suppress accepted findings, fail CI on specific
// categories, and allowlist trusted targets — without editing the scanner.
//
// SECURITY: the policy is the SCANNING USER's, never the scanned target's. It
// is resolved from an explicit --policy path or the current working directory,
// and is NEVER read from inside the target tree. Otherwise a malicious plugin
// could ship its own .assay-policy.json to suppress the very findings Assay
// raises about it. Callers must pass a cwd/flag path, not the target path.
package policy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chawdamrunal/assay/internal/verdict"
)

// DefaultFileName is the policy file auto-discovered in the working directory.
const DefaultFileName = ".assay-policy.json"

// Policy is the parsed .assay-policy.json.
type Policy struct {
	// Suppress silences findings that a team has reviewed and accepted. Each
	// entry requires a reason (enforced at load) so suppressions are auditable,
	// and may carry an expiry so they don't silently outlive their rationale.
	Suppress []Suppression `json:"suppress,omitempty"`
	// DenyCategories fails the gate if any surviving finding is in one of these
	// categories, regardless of severity (e.g. never ship a plugin with any
	// "exfiltration" finding).
	DenyCategories []string `json:"deny_categories,omitempty"`
	// Allowlist names/paths of targets to skip scanning entirely (trusted
	// first-party plugins). Matched against the target's base name or path.
	Allowlist []string `json:"allowlist,omitempty"`
	// FailOn is the default gate threshold when the CLI --fail-on flag is not
	// explicitly set: unsafe | caution | any | off.
	FailOn string `json:"fail_on,omitempty"`
}

// Suppression silences one or more findings.
type Suppression struct {
	// ID is an exact finding ID, or a prefix glob ending in '*'
	// (e.g. "SCA-CVE-2021-*").
	ID string `json:"id"`
	// Reason is REQUIRED — a free-text justification for the audit trail.
	Reason string `json:"reason"`
	// Expiry is an optional YYYY-MM-DD date after which the suppression no
	// longer applies and the finding resurfaces (prevents stale suppressions).
	Expiry string `json:"expiry,omitempty"`
}

// Resolve returns the policy file path to use: the explicit path if non-empty,
// else <cwd>/.assay-policy.json if it exists, else "" (no policy). cwd is the
// user's working directory — NOT the scan target.
func Resolve(explicit, cwd string) string {
	if strings.TrimSpace(explicit) != "" {
		return explicit
	}
	candidate := filepath.Join(cwd, DefaultFileName)
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}

// Load parses and validates a policy file. A malformed policy is an error (it
// should be visible, not silently ignored): every suppression must have a
// reason, every expiry must parse as YYYY-MM-DD, and FailOn (if set) must be a
// known value.
func Load(path string) (*Policy, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- user-supplied policy path by design
	if err != nil {
		return nil, fmt.Errorf("read policy %s: %w", path, err)
	}
	var p Policy
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse policy %s: %w", path, err)
	}
	for i, s := range p.Suppress {
		if strings.TrimSpace(s.ID) == "" {
			return nil, fmt.Errorf("policy suppress[%d]: id is required", i)
		}
		if strings.TrimSpace(s.Reason) == "" {
			return nil, fmt.Errorf("policy suppress[%d] (%s): reason is required so the suppression is auditable", i, s.ID)
		}
		if s.Expiry != "" {
			if _, err := time.Parse("2006-01-02", s.Expiry); err != nil {
				return nil, fmt.Errorf("policy suppress[%d] (%s): expiry %q must be YYYY-MM-DD", i, s.ID, s.Expiry)
			}
		}
	}
	switch strings.ToLower(strings.TrimSpace(p.FailOn)) {
	case "", "off", "never", "none", "any", "caution", "unsafe":
	default:
		return nil, fmt.Errorf("policy fail_on %q invalid (want: unsafe | caution | any | off)", p.FailOn)
	}
	return &p, nil
}

// IsAllowlisted reports whether the target (by base name or full path) is on
// the allowlist and should be skipped. Nil-safe.
func (p *Policy) IsAllowlisted(targetPath string) bool {
	if p == nil {
		return false
	}
	base := filepath.Base(targetPath)
	for _, a := range p.Allowlist {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		if a == base || a == targetPath {
			return true
		}
	}
	return false
}

// Apply removes suppressed (and non-expired) findings, returning the kept set
// and the suppressed set (for reporting "suppressed N per policy"). Nil-safe:
// a nil policy returns findings unchanged.
func (p *Policy) Apply(findings []verdict.Finding, now time.Time) (kept, suppressed []verdict.Finding) {
	if p == nil || len(p.Suppress) == 0 {
		return findings, nil
	}
	for _, f := range findings {
		if p.suppresses(f, now) {
			suppressed = append(suppressed, f)
		} else {
			kept = append(kept, f)
		}
	}
	return kept, suppressed
}

func (p *Policy) suppresses(f verdict.Finding, now time.Time) bool {
	for _, s := range p.Suppress {
		if s.Expiry != "" {
			// Malformed expiry can't reach here — Load rejects it.
			exp, _ := time.Parse("2006-01-02", s.Expiry)
			// Expired (now is after the expiry day): suppression no longer applies.
			if now.After(exp.Add(24 * time.Hour)) {
				continue
			}
		}
		if matchID(s.ID, f.ID) {
			return true
		}
	}
	return false
}

// DeniedCategoryHits returns the findings whose category is in DenyCategories.
// Nil-safe; empty when no deny rules or no matches.
func (p *Policy) DeniedCategoryHits(findings []verdict.Finding) []verdict.Finding {
	if p == nil || len(p.DenyCategories) == 0 {
		return nil
	}
	deny := map[string]bool{}
	for _, c := range p.DenyCategories {
		deny[strings.ToLower(strings.TrimSpace(c))] = true
	}
	var hits []verdict.Finding
	for _, f := range findings {
		if deny[strings.ToLower(f.Category)] {
			hits = append(hits, f)
		}
	}
	return hits
}

// matchID supports exact match or a trailing-'*' prefix glob.
func matchID(pattern, id string) bool {
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(id, strings.TrimSuffix(pattern, "*"))
	}
	return pattern == id
}
