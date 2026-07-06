package verdict

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// sarifInformationURI is advertised as the tool's home in the SARIF driver.
const sarifInformationURI = "https://github.com/chawdamrunal/assay"

// ToSARIF serializes a Verdict into SARIF 2.1.0 JSON. SARIF is the standard
// machine-readable static-analysis format consumed by GitHub Advanced Security
// code scanning, GitLab SAST, the VS Code Problems panel, and DefectDojo — so
// emitting it unlocks all of those CI/IDE surfaces from one serializer.
//
// Mapping decisions:
//   - rule per distinct finding Category (deduped, stable order)
//   - level: critical/high → "error", medium → "warning", low/info → "note"
//   - one SARIF location per Evidence entry (file + startLine)
//   - findings with no Evidence (e.g. a manifest- or description-level issue
//     that has no code line) still emit a result, located at the target root
//     ("." / line 1) so GitHub renders it rather than dropping it. SARIF's
//     location model assumes file:line, so this is the agreed fallback for
//     Assay findings that point at tool-description text or manifest fields.
func ToSARIF(v Verdict) ([]byte, error) {
	rules := buildSARIFRules(v.Findings)
	results := make([]sarifResult, 0, len(v.Findings))
	for _, f := range v.Findings {
		results = append(results, findingToSARIFResult(f))
	}

	version := v.Scanner.Version
	if version == "" {
		version = "0.0.0"
	}

	log := sarifLog{
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
		Version: "2.1.0",
		Runs: []sarifRun{{
			Tool: sarifTool{Driver: sarifDriver{
				Name:           "assay",
				InformationURI: sarifInformationURI,
				Version:        version,
				Rules:          rules,
			}},
			Results: results,
		}},
	}
	return json.MarshalIndent(log, "", "  ")
}

func buildSARIFRules(findings []Finding) []sarifRule {
	seen := map[string]bool{}
	var ids []string
	for _, f := range findings {
		id := ruleID(f.Category)
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	rules := make([]sarifRule, 0, len(ids))
	for _, id := range ids {
		rules = append(rules, sarifRule{
			ID:               id,
			Name:             id,
			ShortDescription: sarifText{Text: fmt.Sprintf("Assay %s finding", id)},
			HelpURI:          sarifInformationURI,
		})
	}
	return rules
}

func findingToSARIFResult(f Finding) sarifResult {
	msg := f.Title
	if f.Description != "" {
		msg = f.Title + " — " + f.Description
	}

	var locs []sarifLocation
	for _, e := range f.Evidence {
		line := max(e.Line, 1)
		uri := e.File
		if uri == "" {
			uri = "."
		}
		locs = append(locs, sarifLocation{PhysicalLocation: sarifPhysicalLocation{
			ArtifactLocation: sarifArtifactLocation{URI: uri},
			Region:           sarifRegion{StartLine: line},
		}})
	}
	if len(locs) == 0 {
		// Fallback so GitHub renders the result rather than discarding it.
		locs = append(locs, sarifLocation{PhysicalLocation: sarifPhysicalLocation{
			ArtifactLocation: sarifArtifactLocation{URI: "."},
			Region:           sarifRegion{StartLine: 1},
		}})
	}

	props := map[string]string{"severity": f.Severity}
	if f.ThreatID != "" {
		props["threat_id"] = f.ThreatID
	}
	if f.RecommendedAction != "" {
		props["recommended_action"] = f.RecommendedAction
	}

	return sarifResult{
		RuleID:     ruleID(f.Category),
		Level:      severityToSARIFLevel(f.Severity),
		Message:    sarifText{Text: msg},
		Locations:  locs,
		Properties: props,
	}
}

// ruleID normalizes a finding Category into a SARIF ruleId. Empty categories
// fall back to a generic id so every result still references a rule.
func ruleID(category string) string {
	c := strings.TrimSpace(category)
	if c == "" {
		return "assay.finding"
	}
	return c
}

// severityToSARIFLevel maps Assay severities onto SARIF's three-value scale.
func severityToSARIFLevel(severity string) string {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "critical", "high":
		return "error"
	case "medium":
		return "warning"
	default: // low, info, unknown
		return "note"
	}
}

// --- SARIF 2.1.0 wire types (minimal subset Assay emits) ---------------------

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	InformationURI string      `json:"informationUri"`
	Version        string      `json:"version"`
	Rules          []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	ShortDescription sarifText `json:"shortDescription"`
	HelpURI          string    `json:"helpUri,omitempty"`
}

type sarifResult struct {
	RuleID     string            `json:"ruleId"`
	Level      string            `json:"level"`
	Message    sarifText         `json:"message"`
	Locations  []sarifLocation   `json:"locations"`
	Properties map[string]string `json:"properties,omitempty"`
}

type sarifText struct {
	Text string `json:"text"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
	Region           sarifRegion           `json:"region"`
}

type sarifArtifactLocation struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine int `json:"startLine"`
}
