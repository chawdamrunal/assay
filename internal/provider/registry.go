package provider

import (
	"fmt"

	"github.com/chawdamrunal/assay/internal/auth"
	"github.com/chawdamrunal/assay/internal/claude"
)

// Registry constructs the LLM "brain" for a given AgentID. Direct-API providers
// (the "-api" ids) become a claude.Client; CLI agents (the "-cli" ids and
// claude-code) have no client and are run via the MCP-spawn path by the caller,
// which branches on IsCLI(). keychainService is where per-provider API keys live.
type Registry struct {
	keychainService string
}

// NewRegistry returns a Registry that reads credentials from the given OS
// keychain service (production: "assay").
func NewRegistry(keychainService string) *Registry {
	return &Registry{keychainService: keychainService}
}

// NewClient builds the BASE direct-API claude.Client for a "-api" provider.
// Callers wrap the result with retry/budget. CLI agents (claude-code,
// gemini-cli, codex-cli) return an error — run them via the MCP-spawn path.
//
// In this build only anthropic-api is wired (reusing the existing Anthropic
// credential chain). gemini-api / openai-api return a clear "not wired yet"
// error so the UI can let users store keys ahead of the client landing.
func (r *Registry) NewClient(id AgentID) (claude.Client, error) {
	switch id.Resolve() {
	case AgentAnthropicAPI:
		// env ANTHROPIC_API_KEY → assay keychain → Claude Code OAuth bearer.
		creds, err := auth.Resolve(r.keychainService)
		if err != nil {
			return nil, err
		}
		return claude.NewRealClientFromCredentials(creds, nil)
	case AgentGeminiAPI, AgentOpenAIAPI:
		return nil, fmt.Errorf("provider %q: direct-API client is not wired in this build yet — set its key in Settings; scanning with it lands shortly", id)
	default:
		return nil, fmt.Errorf("provider %q is not a direct-API provider; run it via the MCP-spawn path", id)
	}
}
