package prepass

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOSVLookupVulnerable(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/query", r.URL.Path)
		var req map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"vulns": []map[string]any{
				{
					"id":      "GHSA-xxxx-yyyy-zzzz",
					"summary": "Remote code execution in vulnerable-pkg",
					"severity": []map[string]any{
						{"type": "CVSS_V3", "score": "9.8/CRITICAL"},
					},
				},
			},
		})
	}))
	defer mock.Close()

	client := &OSVClient{Endpoint: mock.URL + "/v1/query", HTTP: mock.Client()}
	hits, err := client.Lookup("npm", "vulnerable-pkg", "1.0.0")
	require.NoError(t, err)
	require.Len(t, hits, 1)
	assert.Equal(t, "cve", hits[0].Category)
	assert.Contains(t, hits[0].Message, "GHSA-xxxx-yyyy-zzzz")
	assert.Equal(t, "vulnerable-pkg", hits[0].Metadata["package"])
	assert.Equal(t, "critical", hits[0].Severity)
}

func TestOSVLookupNoVulns(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer mock.Close()

	client := &OSVClient{Endpoint: mock.URL + "/v1/query", HTTP: mock.Client()}
	hits, err := client.Lookup("npm", "safe-pkg", "2.0.0")
	require.NoError(t, err)
	assert.Empty(t, hits)
}

func TestOSVLookupNetworkError(t *testing.T) {
	client := &OSVClient{Endpoint: "http://127.0.0.1:1/no-such-server", HTTP: http.DefaultClient}
	_, err := client.Lookup("npm", "anything", "1.0.0")
	require.Error(t, err)
}
