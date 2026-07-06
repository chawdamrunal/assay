package provider

import "testing"

func TestAgentIDTransport(t *testing.T) {
	for _, a := range []AgentID{AgentClaudeCode, AgentGeminiCLI, AgentCodexCLI, ""} {
		if a.Transport() != transportCLI {
			t.Errorf("%q: want cli transport, got %s", a, a.Transport())
		}
		if !a.IsCLI() || a.IsAPI() {
			t.Errorf("%q: IsCLI/IsAPI wrong (cli=%v api=%v)", a, a.IsCLI(), a.IsAPI())
		}
	}
	for _, a := range []AgentID{AgentAnthropicAPI, AgentGeminiAPI, AgentOpenAIAPI} {
		if a.Transport() != transportAPI {
			t.Errorf("%q: want api transport, got %s", a, a.Transport())
		}
		if !a.IsAPI() || a.IsCLI() {
			t.Errorf("%q: IsAPI/IsCLI wrong (api=%v cli=%v)", a, a.IsAPI(), a.IsCLI())
		}
	}
}

func TestAgentIDResolveAndKnown(t *testing.T) {
	if AgentID("").Resolve() != AgentClaudeCode {
		t.Errorf("empty must resolve to %s, got %s", AgentClaudeCode, AgentID("").Resolve())
	}
	if AgentGeminiAPI.Resolve() != AgentGeminiAPI {
		t.Error("non-empty must pass through Resolve unchanged")
	}
	if !AgentClaudeCode.Known() || !AgentID("").Known() {
		t.Error("claude-code and empty (resolves to default) must be Known")
	}
	if AgentID("bogus").Known() {
		t.Error("bogus must not be Known")
	}
}

func TestAPIProvidersAreAllAPI(t *testing.T) {
	for _, a := range APIProviders() {
		if !a.IsAPI() {
			t.Errorf("APIProviders returned a non-api agent: %s", a)
		}
	}
	if len(AllAgents()) != 7 {
		t.Errorf("expected 7 known agents, got %d", len(AllAgents()))
	}
	for _, a := range KeyedAgents() {
		if !a.Known() {
			t.Errorf("KeyedAgents returned an unknown agent: %s", a)
		}
	}
}
