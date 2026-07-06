package store

import (
	"errors"
	"fmt"
	"strings"

	"github.com/zalando/go-keyring"
)

// ErrAPIKeyNotSet is returned when no API key is stored for a provider.
var ErrAPIKeyNotSet = errors.New("API key not set; set it in Settings or run `assay config set api-key`")

// Keyring wraps the OS keychain under a configurable service name, storing one
// API key per provider as a separate keychain account.
type Keyring struct {
	service string
}

// NewKeyring returns a Keyring using the given service name.
// Production callers pass "assay"; tests pass "assay-test".
func NewKeyring(service string) *Keyring {
	return &Keyring{service: service}
}

// providerKeyEntry maps a provider id to its keychain account label. The bare
// vendor prefix is used (transport suffix stripped) so the Anthropic entry
// stays "anthropic-api-key" — the label existing installs and `assay config set
// api-key` already use — preserving backward compatibility. Examples:
// "anthropic-api" → "anthropic-api-key", "gemini-api" → "gemini-api-key".
func providerKeyEntry(providerID string) string { // #nosec G101 -- returns a keychain entry label, not a credential
	v := providerID
	for _, suf := range []string{"-api", "-cli", "-code"} {
		if strings.HasSuffix(v, suf) {
			v = strings.TrimSuffix(v, suf)
			break
		}
	}
	if v == "" {
		v = "anthropic"
	}
	return v + "-api-key"
}

// GetProviderKey reads the stored API key for providerID. Returns ErrAPIKeyNotSet when absent.
func (k *Keyring) GetProviderKey(providerID string) (string, error) {
	v, err := keyring.Get(k.service, providerKeyEntry(providerID))
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrAPIKeyNotSet
	}
	if err != nil {
		return "", fmt.Errorf("keyring get: %w", err)
	}
	return v, nil
}

// SetProviderKey stores the API key for providerID. Rejects empty values.
func (k *Keyring) SetProviderKey(providerID, value string) error {
	if value == "" {
		return errors.New("API key cannot be empty")
	}
	if err := keyring.Set(k.service, providerKeyEntry(providerID), value); err != nil {
		return fmt.Errorf("keyring set: %w", err)
	}
	return nil
}

// DeleteProviderKey removes the stored key for providerID, if any.
func (k *Keyring) DeleteProviderKey(providerID string) error {
	err := keyring.Delete(k.service, providerKeyEntry(providerID))
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("keyring delete: %w", err)
	}
	return nil
}

// HasProviderKey reports whether a non-empty key is stored for providerID.
func (k *Keyring) HasProviderKey(providerID string) bool {
	v, err := k.GetProviderKey(providerID)
	return err == nil && v != ""
}

// --- backward-compatible Anthropic shims (used by the CLI + existing callers) ---

// GetAPIKey reads the Anthropic API key. Returns ErrAPIKeyNotSet when absent.
func (k *Keyring) GetAPIKey() (string, error) { return k.GetProviderKey("anthropic-api") }

// SetAPIKey stores the Anthropic API key. Rejects empty values.
func (k *Keyring) SetAPIKey(value string) error { return k.SetProviderKey("anthropic-api", value) }

// DeleteAPIKey removes the stored Anthropic API key, if any.
func (k *Keyring) DeleteAPIKey() error { return k.DeleteProviderKey("anthropic-api") }

// --- GitHub PAT (for cloning private repos; not an LLM inference provider) ---

// githubTokenProvider is the keychain provider id for the GitHub personal
// access token. Kept separate from the inference providers so it never appears
// in the /api/keys provider list.
const githubTokenProvider = "github-pat"

// GetGitHubToken reads the stored GitHub PAT. Returns ErrAPIKeyNotSet when absent.
func (k *Keyring) GetGitHubToken() (string, error) { return k.GetProviderKey(githubTokenProvider) }

// SetGitHubToken stores the GitHub PAT. Rejects empty values.
func (k *Keyring) SetGitHubToken(value string) error {
	return k.SetProviderKey(githubTokenProvider, value)
}

// DeleteGitHubToken removes the stored GitHub PAT, if any.
func (k *Keyring) DeleteGitHubToken() error { return k.DeleteProviderKey(githubTokenProvider) }

// HasGitHubToken reports whether a GitHub PAT is stored.
func (k *Keyring) HasGitHubToken() bool { return k.HasProviderKey(githubTokenProvider) }
