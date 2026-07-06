package claude

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chawdamrunal/assay/internal/auth"
)

func TestClientInterfaceShape(t *testing.T) {
	// Compile-time assertion: FakeClient satisfies Client.
	var _ Client = (*FakeClient)(nil)

	fc := NewFakeClient()
	fc.Enqueue(Response{Text: "hello", Stop: "end_turn"})

	resp, err := fc.Complete(context.Background(), Request{
		Model:    "claude-sonnet-4-6",
		System:   "you are a test",
		Messages: []Message{{Role: "user", Content: []Content{{Type: "text", Text: "hi"}}}},
	})
	require.NoError(t, err)
	assert.Equal(t, "hello", resp.Text)
	assert.Equal(t, "end_turn", resp.Stop)
}

func TestFakeClientExhausted(t *testing.T) {
	fc := NewFakeClient()
	_, err := fc.Complete(context.Background(), Request{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no enqueued response")
}

func TestNewRealClientFromCredentialsAPIKey(t *testing.T) {
	c, err := NewRealClientFromCredentials(&auth.Credentials{ // #nosec G101 -- test fixture, not a real credential
		Kind:   auth.KindAPIKey,
		APIKey: "sk-ant-test",
		Source: auth.MethodEnv,
	}, nil)
	require.NoError(t, err)
	require.NotNil(t, c)
}

func TestNewRealClientFromCredentialsBearer(t *testing.T) {
	c, err := NewRealClientFromCredentials(&auth.Credentials{ // #nosec G101 -- test fixture, not a real credential
		Kind:        auth.KindBearer,
		BearerToken: "sk-ant-oat01-test",
		Source:      auth.MethodClaudeCode,
	}, nil)
	require.NoError(t, err)
	require.NotNil(t, c)
}

func TestNewRealClientFromCredentialsNil(t *testing.T) {
	_, err := NewRealClientFromCredentials(nil, nil)
	require.Error(t, err)
}

func TestNewRealClientFromCredentialsEmptyKey(t *testing.T) {
	_, err := NewRealClientFromCredentials(&auth.Credentials{Kind: auth.KindAPIKey, APIKey: ""}, nil)
	require.Error(t, err)
}

func TestNewRealClientFromCredentialsEmptyBearer(t *testing.T) {
	_, err := NewRealClientFromCredentials(&auth.Credentials{Kind: auth.KindBearer, BearerToken: ""}, nil)
	require.Error(t, err)
}
