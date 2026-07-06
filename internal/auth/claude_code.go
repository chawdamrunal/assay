package auth

import (
	"encoding/json"
	"errors"
	"os/user"
	"time"

	"github.com/zalando/go-keyring"
)

// ClaudeCodeKeychainService is the macOS Keychain service name Claude Code uses.
const ClaudeCodeKeychainService = "Claude Code-credentials"

// claudeCodeBlob mirrors the JSON structure stored in the Claude Code keychain entry.
// We only care about the claudeAiOauth field; mcpOAuth is for MCP servers and irrelevant here.
type claudeCodeBlob struct {
	ClaudeAiOauth struct {
		AccessToken      string   `json:"accessToken"`
		RefreshToken     string   `json:"refreshToken"`
		ExpiresAt        int64    `json:"expiresAt"` // milliseconds since epoch
		Scopes           []string `json:"scopes"`
		SubscriptionType string   `json:"subscriptionType"`
		RateLimitTier    string   `json:"rateLimitTier"`
	} `json:"claudeAiOauth"`
}

// claudeCodeReader is a function so tests can inject a fake. Default reads OS keychain.
var claudeCodeReader = readClaudeCodeFromOSKeychain

// FromClaudeCode tries to read the user's existing Claude Code OAuth credentials.
// Returns nil if Claude Code is not installed, no entry exists, or access is denied.
// Returns nil with a logged hint if the token has expired (we don't refresh; that's
// Claude Code's job).
func FromClaudeCode() *Credentials {
	raw, err := claudeCodeReader()
	if err != nil {
		return nil
	}
	if raw == "" {
		return nil
	}
	var blob claudeCodeBlob
	if err := json.Unmarshal([]byte(raw), &blob); err != nil {
		return nil
	}
	if blob.ClaudeAiOauth.AccessToken == "" {
		return nil
	}
	expiresAt := time.Unix(blob.ClaudeAiOauth.ExpiresAt/1000, 0)
	if time.Now().After(expiresAt) {
		// Token expired — caller should re-launch Claude Code to refresh.
		return nil
	}
	return &Credentials{
		Kind:         KindBearer,
		BearerToken:  blob.ClaudeAiOauth.AccessToken,
		Source:       MethodClaudeCode,
		ExpiresAt:    expiresAt,
		Subscription: blob.ClaudeAiOauth.SubscriptionType,
	}
}

// readClaudeCodeFromOSKeychain reads the keychain entry. Account is the OS
// username (Claude Code stores under the current user's keychain).
func readClaudeCodeFromOSKeychain() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	v, err := keyring.Get(ClaudeCodeKeychainService, u.Username)
	if err != nil {
		// Includes "not found" and "access denied"
		if errors.Is(err, keyring.ErrNotFound) {
			return "", nil
		}
		return "", err
	}
	return v, nil
}

// SetClaudeCodeReaderForTesting replaces the function that reads Claude Code's
// keychain entry. Tests use this to inject a known JSON blob; production callers
// should not touch it.
func SetClaudeCodeReaderForTesting(fn func() (string, error)) {
	claudeCodeReader = fn
}

// ResetClaudeCodeReaderForTesting restores the production reader.
func ResetClaudeCodeReaderForTesting() {
	claudeCodeReader = readClaudeCodeFromOSKeychain
}
