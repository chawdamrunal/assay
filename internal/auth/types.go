// Package auth resolves Anthropic credentials from multiple sources, in
// priority order: explicit env var, Assay-managed keychain entry, and the
// user's existing Claude Code OAuth credentials (auto-detected).
//
// This lets Claude Code subscribers (Pro/Max) use Assay without obtaining a
// separate API key — Assay re-uses the bearer token that Claude Code already
// negotiated via OAuth.
package auth

import (
	"errors"
	"time"
)

// Method identifies which source produced the active credentials.
type Method string

// Auth method identifiers, in resolver priority order.
const (
	MethodEnv        Method = "env"         // ANTHROPIC_API_KEY env var
	MethodAssayKey   Method = "assay-key"   // assay config set api-key (OS keychain)
	MethodClaudeCode Method = "claude-code" // re-uses Claude Code's OAuth bearer
)

// Kind is "api-key" (x-api-key header) or "bearer" (Authorization: Bearer header).
type Kind string

// Credential kinds — selects which Anthropic auth header is used.
const (
	KindAPIKey Kind = "api-key"
	KindBearer Kind = "bearer"
)

// Credentials carries everything the Anthropic client needs to authenticate.
// Exactly one of APIKey or BearerToken is set, indicated by Kind.
type Credentials struct {
	Kind         Kind
	APIKey       string // when Kind == KindAPIKey
	BearerToken  string // when Kind == KindBearer
	Source       Method
	ExpiresAt    time.Time // zero for API keys; populated for OAuth
	Subscription string    // e.g., "max", "pro" — display only
}

// Expired reports whether ExpiresAt is set and in the past.
func (c *Credentials) Expired() bool {
	return !c.ExpiresAt.IsZero() && time.Now().After(c.ExpiresAt)
}

// ErrNoCredentials is returned when no auth method has credentials available.
var ErrNoCredentials = errors.New("no Anthropic credentials available; set ANTHROPIC_API_KEY, run `assay config set api-key`, or log into Claude Code")
