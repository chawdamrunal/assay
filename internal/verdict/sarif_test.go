package verdict

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToSARIF(t *testing.T) {
	v := Verdict{
		Scanner: Scanner{Version: "1.2.3"},
		Findings: []Finding{
			{
				ID: "F-001", Severity: "high", Category: "exfiltration",
				Title: "Reads AWS creds", Description: "reads ~/.aws/credentials", ThreatID: "T1",
				Evidence: []Evidence{{File: "src/main.js", Line: 23, Snippet: "fs.readFileSync"}},
			},
			{
				ID: "F-002", Severity: "medium", Category: "overscope",
				Title: "Broad permission grant", // no evidence → fallback location
			},
		},
	}

	raw, err := ToSARIF(v)
	require.NoError(t, err)

	var log struct {
		Version string `json:"version"`
		Runs    []struct {
			Tool struct {
				Driver struct {
					Name    string `json:"name"`
					Version string `json:"version"`
					Rules   []struct {
						ID string `json:"id"`
					} `json:"rules"`
				} `json:"driver"`
			} `json:"tool"`
			Results []struct {
				RuleID    string `json:"ruleId"`
				Level     string `json:"level"`
				Locations []struct {
					PhysicalLocation struct {
						ArtifactLocation struct {
							URI string `json:"uri"`
						} `json:"artifactLocation"`
						Region struct {
							StartLine int `json:"startLine"`
						} `json:"region"`
					} `json:"physicalLocation"`
				} `json:"locations"`
			} `json:"results"`
		} `json:"runs"`
	}
	require.NoError(t, json.Unmarshal(raw, &log))

	assert.Equal(t, "2.1.0", log.Version)
	require.Len(t, log.Runs, 1)
	run := log.Runs[0]
	assert.Equal(t, "assay", run.Tool.Driver.Name)
	assert.Equal(t, "1.2.3", run.Tool.Driver.Version)
	assert.Len(t, run.Tool.Driver.Rules, 2, "two distinct categories → two rules")
	require.Len(t, run.Results, 2)

	// high → error, with the real file:line location.
	assert.Equal(t, "exfiltration", run.Results[0].RuleID)
	assert.Equal(t, "error", run.Results[0].Level)
	require.Len(t, run.Results[0].Locations, 1)
	assert.Equal(t, "src/main.js", run.Results[0].Locations[0].PhysicalLocation.ArtifactLocation.URI)
	assert.Equal(t, 23, run.Results[0].Locations[0].PhysicalLocation.Region.StartLine)

	// medium → warning, with the no-evidence fallback location so GitHub still
	// renders it.
	assert.Equal(t, "warning", run.Results[1].Level)
	require.Len(t, run.Results[1].Locations, 1)
	assert.Equal(t, ".", run.Results[1].Locations[0].PhysicalLocation.ArtifactLocation.URI)
	assert.Equal(t, 1, run.Results[1].Locations[0].PhysicalLocation.Region.StartLine)
}

func TestSeverityToSARIFLevel(t *testing.T) {
	assert.Equal(t, "error", severityToSARIFLevel("critical"))
	assert.Equal(t, "error", severityToSARIFLevel("high"))
	assert.Equal(t, "warning", severityToSARIFLevel("medium"))
	assert.Equal(t, "note", severityToSARIFLevel("low"))
	assert.Equal(t, "note", severityToSARIFLevel("info"))
	assert.Equal(t, "note", severityToSARIFLevel("")) // unknown → note
}
