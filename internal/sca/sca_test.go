package sca

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chawdamrunal/assay/internal/prepass"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWalkManifestsParsesPackageJSON(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "package.json"), []byte(`{
		"name": "toy",
		"dependencies": { "express": "4.17.1", "lodash": "^4.17.21" },
		"devDependencies": { "mocha": "8.0.0" }
	}`), 0o600))

	coords, err := walkManifests(tmp)
	require.NoError(t, err)
	names := names(coords)
	assert.Contains(t, names, "express@4.17.1")
	assert.Contains(t, names, "lodash@4.17.21") // ^ stripped
	assert.Contains(t, names, "mocha@8.0.0")
}

func TestWalkManifestsParsesGoSum(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.sum"), []byte(
		"github.com/foo/bar v1.2.3 h1:abcdef=\n"+
			"github.com/foo/bar v1.2.3/go.mod h1:abcdef=\n"+
			"golang.org/x/crypto v0.0.0-20200622213623-75b288015ac9 h1:zzz=\n"+
			"golang.org/x/crypto v0.0.0-20200622213623-75b288015ac9/go.mod h1:zzz=\n"), 0o600))

	coords, err := walkManifests(tmp)
	require.NoError(t, err)
	n := names(coords)
	assert.Contains(t, n, "github.com/foo/bar@v1.2.3")
	assert.Contains(t, n, "golang.org/x/crypto@v0.0.0-20200622213623-75b288015ac9")
	for _, c := range coords {
		assert.Equal(t, "Go", c.Ecosystem)
	}
	// The two go.sum lines per module (with/without /go.mod) must dedup.
	assert.Len(t, coords, 2)
}

func TestWalkManifestsParsesCargoLock(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "Cargo.lock"), []byte(`# auto-generated
version = 3

[[package]]
name = "serde"
version = "1.0.197"

[[package]]
name = "tokio"
version = "1.36.0"
`), 0o600))

	coords, err := walkManifests(tmp)
	require.NoError(t, err)
	n := names(coords)
	assert.Contains(t, n, "serde@1.0.197")
	assert.Contains(t, n, "tokio@1.36.0")
	for _, c := range coords {
		assert.Equal(t, "crates.io", c.Ecosystem)
	}
}

func TestWalkManifestsParsesPackageLockJSON(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "package-lock.json"), []byte(`{
		"name": "toy",
		"lockfileVersion": 3,
		"packages": {
			"node_modules/express":   { "version": "4.17.1" },
			"node_modules/lodash":    { "version": "4.17.21" },
			"node_modules/express/node_modules/cookie": { "version": "0.4.0" }
		}
	}`), 0o600))
	coords, err := walkManifests(tmp)
	require.NoError(t, err)
	names := names(coords)
	assert.Contains(t, names, "express@4.17.1")
	assert.Contains(t, names, "lodash@4.17.21")
	assert.Contains(t, names, "cookie@0.4.0") // nested transitive
}

func TestWalkManifestsParsesRequirementsTxt(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "requirements.txt"), []byte(`# comment
requests==2.31.0
flask==2.0.1
loose-pin>=1.0   # this one we skip — no exact version
`), 0o600))
	coords, err := walkManifests(tmp)
	require.NoError(t, err)
	names := names(coords)
	assert.Contains(t, names, "requests@2.31.0")
	assert.Contains(t, names, "flask@2.0.1")
	assert.NotContains(t, strings.Join(names, ","), "loose-pin")
}

func TestWalkManifestsSkipsNodeModules(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "node_modules", "lodash"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "node_modules", "lodash", "package.json"),
		[]byte(`{"name":"lodash","dependencies":{"NESTED_SHOULD_NOT_APPEAR":"1.0.0"}}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "package.json"),
		[]byte(`{"name":"toy","dependencies":{"lodash":"4.17.21"}}`), 0o600))
	coords, err := walkManifests(tmp)
	require.NoError(t, err)
	for _, c := range coords {
		assert.NotEqual(t, "NESTED_SHOULD_NOT_APPEAR", c.Name,
			"walker must skip node_modules to avoid blowing up on big trees")
	}
}

func TestAnalyzeOfflineSkipsNetwork(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "package.json"),
		[]byte(`{"name":"toy","dependencies":{"lodash":"4.17.21"}}`), 0o600))
	res, err := Analyze(context.Background(), tmp, true, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, res.DepCount)
	assert.Empty(t, res.Findings, "offline mode must NOT call OSV")
	assert.Contains(t, res.SkipReason, "offline")
}

func TestAnalyzeReturnsFindingsFromMockOSV(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "package.json"),
		[]byte(`{"name":"toy","dependencies":{"vulnerable-pkg":"1.0.0"}}`), 0o600))

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"vulns":[{"id":"GHSA-test-1234","summary":"test vuln","severity":[{"type":"CVSS_V3","score":"CRITICAL"}]}]}`))
	}))
	defer mock.Close()
	osv := &prepass.OSVClient{Endpoint: mock.URL + "/v1/query", HTTP: mock.Client()}

	res, err := Analyze(context.Background(), tmp, false, osv)
	require.NoError(t, err)
	require.Len(t, res.Findings, 1)
	f := res.Findings[0]
	assert.Equal(t, "critical", f.Severity)
	assert.Equal(t, "dependency", f.Category)
	assert.Contains(t, f.Title, "vulnerable-pkg")
	assert.Contains(t, f.Title, "GHSA-test-1234")
}

func TestCleanVersionStripsRangePrefixes(t *testing.T) {
	cases := map[string]string{
		"^4.17.21":        "4.17.21",
		"~1.2.3":          "1.2.3",
		">=1.0.0 <2.0.0":  "1.0.0",
		"*":               "",
		"file:./local":    "",
		"git+https://...": "",
		"  3.0.0  ":       "3.0.0",
	}
	for in, want := range cases {
		assert.Equal(t, want, cleanVersion(in), "input=%q", in)
	}
}

// names returns a "name@version" string per coord so tests can read.
func names(cs []Coord) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.Name+"@"+c.Version)
	}
	return out
}
