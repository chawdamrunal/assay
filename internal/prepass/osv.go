package prepass

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OSVClient queries the public OSV.dev API for known vulnerabilities by
// (ecosystem, package, version).
type OSVClient struct {
	Endpoint string
	HTTP     *http.Client
}

// DefaultOSV returns a client pointed at the public api.osv.dev.
func DefaultOSV() *OSVClient {
	return &OSVClient{
		Endpoint: "https://api.osv.dev/v1/query",
		HTTP:     &http.Client{Timeout: 10 * time.Second},
	}
}

type osvQuery struct {
	Package osvPkg `json:"package"`
	Version string `json:"version"`
}

type osvPkg struct {
	Name      string `json:"name"`
	Ecosystem string `json:"ecosystem"`
}

type osvResponse struct {
	Vulns []osvVuln `json:"vulns"`
}

type osvVuln struct {
	ID       string        `json:"id"`
	Summary  string        `json:"summary"`
	Severity []osvSeverity `json:"severity"`
}

type osvSeverity struct {
	Type  string `json:"type"`
	Score string `json:"score"`
}

// Lookup queries OSV for the given coordinate. Returns one Hit per Vuln,
// or empty if nothing was found. Network errors are returned.
func (c *OSVClient) Lookup(ecosystem, name, version string) ([]Hit, error) {
	body, err := json.Marshal(osvQuery{
		Package: osvPkg{Name: name, Ecosystem: ecosystem},
		Version: version,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal osv query: %w", err)
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, c.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build osv request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("osv request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("osv returned %d: %s", resp.StatusCode, raw)
	}

	var parsed osvResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode osv response: %w", err)
	}

	hits := make([]Hit, 0, len(parsed.Vulns))
	for _, v := range parsed.Vulns {
		severity := "medium"
		for _, s := range v.Severity {
			if strings.Contains(strings.ToUpper(s.Score), "CRITICAL") {
				severity = "critical"
			} else if strings.Contains(strings.ToUpper(s.Score), "HIGH") {
				severity = "high"
			}
		}
		hits = append(hits, Hit{
			Category: "cve",
			Severity: severity,
			Message:  fmt.Sprintf("%s: %s", v.ID, v.Summary),
			Metadata: map[string]string{
				"package":   name,
				"version":   version,
				"ecosystem": ecosystem,
				"id":        v.ID,
			},
		})
	}
	return hits, nil
}
