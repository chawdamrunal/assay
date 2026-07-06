package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chawdamrunal/assay/internal/prepass"
)

func TestOSVToolWrapsClient(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"vulns": []map[string]any{
				{"id": "GHSA-test", "summary": "test vuln", "severity": []map[string]any{{"type": "CVSS_V3", "score": "9.0/CRITICAL"}}},
			},
		})
	}))
	defer mock.Close()

	client := &prepass.OSVClient{Endpoint: mock.URL + "/v1/query", HTTP: mock.Client()}
	tool := NewOSVTool(client)

	r, err := tool.Lookup(context.Background(), Invocation{Input: map[string]any{
		"ecosystem": "npm",
		"package":   "vulnerable-pkg",
		"version":   "1.0.0",
	}})
	require.NoError(t, err)
	assert.Contains(t, r.Text, "GHSA-test")
	assert.Contains(t, r.Text, "critical")
}

func TestOSVToolMissingArgs(t *testing.T) {
	tool := NewOSVTool(prepass.DefaultOSV())
	_, err := tool.Lookup(context.Background(), Invocation{Input: map[string]any{}})
	require.Error(t, err)
}
