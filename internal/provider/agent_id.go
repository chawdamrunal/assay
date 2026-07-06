// Package provider defines the LLM "brains" Assay can scan with and the
// machinery to construct them. There are two transports:
//
//   - CLI agents that drive the Assay MCP server by spawning a CLI process
//     (claude-code, gemini-cli, codex-cli). The MCP server is the brain's
//     toolset; any MCP-capable CLI can run it.
//   - Direct-API providers that implement the claude.Client interface and act
//     as the brain in-process (anthropic-api, gemini-api, openai-api).
//
// The default is claude-code (the Claude Code subscription via the MCP-spawn
// path) — it needs no API key. Every other agent is opt-in: the user selects it
// in config or per-scan and supplies a key (for the -api providers) or installs
// the CLI (for the -cli agents).
package provider

import "strings"

// AgentID names one LLM brain by transport × vendor. The "-cli" suffix marks a
// spawned CLI agent that drives the MCP server; the "-api" suffix marks a
// direct-API provider that implements claude.Client. The empty string resolves
// to AgentClaudeCode (the default).
type AgentID string

// Recognized agent ids. Keep in sync with the target/provider values the web UI
// offers and the keychain entry labels (<provider>-api-key) in internal/store.
const (
	AgentClaudeCode   AgentID = "claude-code"   // CLI-spawn: `claude -p` (DEFAULT)
	AgentCursorAgent  AgentID = "cursor-agent"  // CLI-spawn: `cursor-agent -p`
	AgentGeminiCLI    AgentID = "gemini-cli"    // CLI-spawn: `gemini`
	AgentCodexCLI     AgentID = "codex-cli"     // CLI-spawn: `codex`
	AgentAnthropicAPI AgentID = "anthropic-api" // direct Anthropic API
	AgentGeminiAPI    AgentID = "gemini-api"    // direct Gemini API
	AgentOpenAIAPI    AgentID = "openai-api"    // direct OpenAI API
)

// DefaultAgent is used when config or a scan request leave the agent unset.
const DefaultAgent = AgentClaudeCode

const (
	transportCLI = "cli"
	transportAPI = "api"
)

// Resolve maps the empty string to the default agent; other values pass through
// unchanged. Use it at every boundary that reads a user-supplied agent value.
func (a AgentID) Resolve() AgentID {
	if a == "" {
		return DefaultAgent
	}
	return a
}

// Transport reports how the agent runs: "cli" (a spawned process driving the
// MCP server) or "api" (an in-process claude.Client). Empty/unknown ids resolve
// to the default's transport ("cli").
func (a AgentID) Transport() string {
	switch a.Resolve() {
	case AgentAnthropicAPI, AgentGeminiAPI, AgentOpenAIAPI:
		return transportAPI
	default: // claude-code, gemini-cli, codex-cli
		return transportCLI
	}
}

// IsAPI reports whether the agent runs as a direct-API claude.Client.
func (a AgentID) IsAPI() bool { return a.Transport() == transportAPI }

// IsCLI reports whether the agent runs by spawning a CLI that drives the MCP server.
func (a AgentID) IsCLI() bool { return a.Transport() == transportCLI }

// Known reports whether a is a recognized agent id (after resolving empty).
func (a AgentID) Known() bool {
	switch a.Resolve() {
	case AgentClaudeCode, AgentCursorAgent, AgentGeminiCLI, AgentCodexCLI, AgentAnthropicAPI, AgentGeminiAPI, AgentOpenAIAPI:
		return true
	default:
		return false
	}
}

// AllAgents lists every recognized agent id, default first. Drives the UI
// provider list and per-provider status probes.
func AllAgents() []AgentID {
	return []AgentID{
		AgentClaudeCode,
		AgentCursorAgent,
		AgentGeminiCLI,
		AgentCodexCLI,
		AgentAnthropicAPI,
		AgentGeminiAPI,
		AgentOpenAIAPI,
	}
}

// APIProviders lists the direct-API agent ids (those that hold an API key).
func APIProviders() []AgentID {
	return []AgentID{AgentAnthropicAPI, AgentGeminiAPI, AgentOpenAIAPI}
}

// KeyedAgents lists the agents a user can store an API key for. CLI agents
// inject the key as their env var at spawn (ANTHROPIC_API_KEY / CURSOR_API_KEY /
// GEMINI_API_KEY / OPENAI_API_KEY); the key is optional for claude-code, which
// can also use its subscription bearer.
func KeyedAgents() []AgentID {
	return []AgentID{AgentClaudeCode, AgentCursorAgent, AgentGeminiCLI, AgentCodexCLI}
}

// Vendor returns the bare vendor name without the transport suffix:
// "anthropic-api" → "anthropic", "gemini-cli" → "gemini", "claude-code" →
// "claude". Used for the keychain entry label and the per-provider status kind.
func (a AgentID) Vendor() string {
	s := string(a.Resolve())
	for _, suf := range []string{"-api", "-cli", "-code"} {
		if strings.HasSuffix(s, suf) {
			return strings.TrimSuffix(s, suf)
		}
	}
	return s
}
