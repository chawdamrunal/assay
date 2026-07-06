package claude

import (
	"encoding/json"
	"fmt"
	"os"
)

// fixtureToolUse mirrors ToolUse with explicit snake_case JSON tags so
// recording files can use natural API-shaped key names.
type fixtureToolUse struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

// fixtureResponse mirrors Response for unmarshaling from JSON recordings.
// Response itself has no JSON tags; this is the on-disk wire shape.
type fixtureResponse struct {
	Text     string           `json:"text"`
	ToolUses []fixtureToolUse `json:"tool_uses"`
	Stop     string           `json:"stop"`
}

func (r fixtureResponse) toResponse() Response {
	uses := make([]ToolUse, len(r.ToolUses))
	for i, u := range r.ToolUses {
		uses[i] = ToolUse(u)
	}
	return Response{Text: r.Text, ToolUses: uses, Stop: r.Stop}
}

// Fixture is a pre-recorded scan transcript stored at
// testdata/recorded/<name>.json. Used by both the integration test suite and
// the `assay serve --fake` mode so the live UI can demonstrate the pipeline
// without burning real API tokens.
type Fixture struct {
	Fixture            string            `json:"fixture"`
	ExpectedVerdict    string            `json:"expected_verdict"`
	ExpectedCategories []string          `json:"expected_categories"`
	Responses          []fixtureResponse `json:"responses"`
}

// LoadFixture reads a recorded scan transcript from disk.
func LoadFixture(path string) (*Fixture, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- caller-supplied path (test/dev tooling)
	if err != nil {
		return nil, fmt.Errorf("read fixture %s: %w", path, err)
	}
	var f Fixture
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse fixture %s: %w", path, err)
	}
	return &f, nil
}

// NewFakeClientFromFixture returns a FakeClient pre-loaded with the fixture's
// recorded responses, ready to drive a single end-to-end scan.
func NewFakeClientFromFixture(f *Fixture) *FakeClient {
	fc := NewFakeClient()
	for _, r := range f.Responses {
		fc.Enqueue(r.toResponse())
	}
	return fc
}
