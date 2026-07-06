package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/chawdamrunal/assay/internal/assistant"
	"github.com/chawdamrunal/assay/internal/github"
	"github.com/chawdamrunal/assay/internal/inventory"
)

// AssistantDeps are the dependencies the /api/assistant routes need.
//
// The handler reuses the existing scan-start path so a chat-confirmed scan is
// indistinguishable from a New Scan picker click downstream: same scan id
// shape, same SSE events, same audit.json output.
type AssistantDeps struct {
	LoadInventory   func() (inventory.Inventory, error)
	MarketplacesDir string
	Store           *assistant.ConversationStore
	StartScan       StartScanFunc
	Runner          *ScanRunner
	// AllowedRoots is the path-guard whitelist applied before kicking off a
	// confirmed scan. Defense-in-depth: candidate paths come from the local
	// resolver so they're already trusted, but the guard catches future
	// resolver changes that might surface a path the user shouldn't scan.
	AllowedRoots []string
	// GitHubCacheDir is the directory where on-demand GitHub clones land
	// (typically ~/.assay/sources/). When empty, GitHub auto-fetch is
	// disabled and the handler falls back to the v0.5 "tell the user to
	// install locally" reply.
	GitHubCacheDir string
	// GitHubToken lazily resolves a GitHub PAT (keychain → env → gh CLI) so
	// the on-demand clone can reach private repos. nil disables authenticated
	// clones (the handler then fetches public repos only).
	GitHubToken github.TokenFunc
}

// AssistantRequest is the body of POST /api/assistant/message.
type AssistantRequest struct {
	Text           string `json:"text"`
	ConversationID string `json:"conversation_id,omitempty"`
}

// AssistantReply is the discriminated response shape.
//
// Kinds:
//
//	text          — Assay has nothing to do; just speak.
//	proposal      — Assay found one or more candidates. UI renders cards.
//	scan_started  — A scan has been kicked off; UI should embed live progress.
//	error         — Something went wrong; the message contains the reason.
type AssistantReply struct {
	Kind           string                `json:"kind"`
	Text           string                `json:"text"`
	ConversationID string                `json:"conversation_id"`
	Candidates     []assistant.Candidate `json:"candidates,omitempty"`
	// Suggestions are soft "did you mean?" names returned alongside an
	// empty Candidates list so the UI can render small clickable hints
	// without re-running the resolver client-side.
	Suggestions []assistant.Suggestion `json:"suggestions,omitempty"`
	ScanID      string                 `json:"scan_id,omitempty"`
	Target      string                 `json:"target,omitempty"`
	// GithubURL, when set, is the URL the user mentioned that Assay can't
	// fetch yet. The UI renders it as a link so the user can open it in
	// a browser.
	GithubURL string `json:"github_url,omitempty"`
}

// NewAssistantHandler constructs the /api/assistant/message handler.
func NewAssistantHandler(d AssistantDeps) http.Handler {
	resolver := &assistant.Resolver{
		LoadInventory:   d.LoadInventory,
		MarketplacesDir: d.MarketplacesDir,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			WriteJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req AssistantRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteJSONError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
			return
		}
		req.Text = strings.TrimSpace(req.Text)
		if req.Text == "" {
			WriteJSONError(w, http.StatusBadRequest, "text is required")
			return
		}

		conv := d.Store.GetOrCreate(req.ConversationID)
		intent := assistant.ParseIntent(req.Text)
		reply := dispatch(d, resolver, conv, intent, req.Text)
		reply.ConversationID = conv.ID
		d.Store.Set(conv)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(reply)
	})
}

// dispatch maps an intent to a reply. Pure function over the conversation's
// state — no I/O outside what the resolver and StartScan callbacks already do.
func dispatch(
	d AssistantDeps,
	resolver *assistant.Resolver,
	conv *assistant.Conversation,
	intent assistant.Intent,
	rawText string,
) AssistantReply {
	switch intent.Action {
	case assistant.ActionHelp:
		return AssistantReply{
			Kind: "text",
			Text: helpText(),
		}

	case assistant.ActionList:
		return listReply(d)

	case assistant.ActionDeny:
		conv.PendingCandidates = nil
		conv.LastQueriedTarget = ""
		return AssistantReply{
			Kind: "text",
			Text: "OK, I'll stop. Tell me which plugin or MCP server you want me to check whenever you're ready.",
		}

	case assistant.ActionConfirm:
		return confirmReply(d, conv, intent.Index)

	case assistant.ActionScan:
		return scanReply(d, resolver, conv, intent.Target)

	case assistant.ActionScanGitHub:
		return githubReply(d, resolver, conv, intent)

	default:
		return AssistantReply{
			Kind: "text",
			Text: didNotUnderstand(rawText),
		}
	}
}

// githubReply handles ActionScanGitHub. Assay now (v0.6) actually fetches
// the source: when the local resolver misses we clone the repo into the
// configured cache dir and kick off a scan immediately. No "install it
// locally first" hand-off — the assistant does the work.
//
// The flow:
//  1. If the repo name matches something already installed, propose those
//     candidates first (the running local copy is the more interesting
//     scan target — that's what the user actually exposes to Claude Code).
//  2. Otherwise shallow-clone github.com/<owner>/<repo> into the cache,
//     start a scan against that path, and return a scan_started reply so
//     the FE embeds the live progress stream.
//  3. If git is missing or the clone fails, fall back to the v0.5 "give
//     the user instructions" reply so they're never stranded.
func githubReply(d AssistantDeps, resolver *assistant.Resolver, conv *assistant.Conversation, intent assistant.Intent) AssistantReply {
	cands, _ := resolver.Resolve(intent.GithubRepo)
	conv.LastQueriedTarget = intent.GithubRepo
	if len(cands) > 0 {
		conv.PendingCandidates = cands
		return AssistantReply{
			Kind:       "proposal",
			Text:       fmt.Sprintf("That's the GitHub repo for **%s/%s**. I also found %d local match(es) under the same name — want me to scan one of those (faster, exact code you're running), or click the GitHub option below to fetch and scan upstream HEAD?", intent.GithubOwner, intent.GithubRepo, len(cands)),
			Target:     intent.GithubRepo,
			Candidates: cands,
			GithubURL:  intent.GithubURL,
		}
	}

	// No local match — auto-clone from GitHub and scan, as long as the
	// caller wired the cache dir.
	conv.PendingCandidates = nil
	if d.GitHubCacheDir == "" {
		return manualInstructionsReply(intent)
	}
	if err := github.EnsureGitInstalled(); err != nil {
		return AssistantReply{
			Kind: "error",
			Text: fmt.Sprintf(
				"I'd auto-fetch **%s** from GitHub, but `git` isn't on the server's PATH (%s). Install git and try again, or clone the repo manually and paste the local path.",
				intent.GithubURL, err.Error(),
			),
			GithubURL: intent.GithubURL,
		}
	}

	cloneCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	res, err := github.Clone(cloneCtx, intent.GithubOwner, intent.GithubRepo, d.GitHubCacheDir, d.GitHubToken)
	if err != nil {
		return AssistantReply{
			Kind: "error",
			Text: fmt.Sprintf(
				"I tried to fetch **%s** but `git clone` failed:\n\n```\n%s\n```\n\nThis usually means the repo is private (no auth), the name is mistyped, or your network blocked github.com. Try again or paste a local path.",
				intent.GithubURL, truncateForDisplay(err.Error(), 300),
			),
			GithubURL: intent.GithubURL,
		}
	}

	// Hand the cloned path to the existing scan dispatcher. The
	// path-guard inside startScan validates it lives under an allowed
	// root (we ship GitHubCacheDir as one of those roots from cmd_serve).
	scanID, err := startScan(d, res.LocalPath)
	if err != nil {
		return AssistantReply{
			Kind: "error",
			Text: fmt.Sprintf("I cloned **%s/%s** to `%s` but couldn't start the scan: %s",
				intent.GithubOwner, intent.GithubRepo, res.LocalPath, err.Error()),
			GithubURL: intent.GithubURL,
		}
	}

	shaSuffix := ""
	if res.CommitSHA != "" && len(res.CommitSHA) >= 7 {
		shaSuffix = " at commit `" + res.CommitSHA[:7] + "`"
	}
	return AssistantReply{
		Kind: "scan_started",
		Text: fmt.Sprintf(
			"Got it — I cloned **%s/%s**%s and started the scan. Live progress below.",
			intent.GithubOwner, intent.GithubRepo, shaSuffix,
		),
		Target:    intent.GithubRepo,
		ScanID:    scanID,
		GithubURL: intent.GithubURL,
	}
}

// manualInstructionsReply is the v0.5 fallback reply for environments where
// auto-fetch is disabled (no cache dir configured). Kept around so tests
// and edge deployments degrade gracefully instead of erroring.
func manualInstructionsReply(intent assistant.Intent) AssistantReply {
	return AssistantReply{
		Kind: "text",
		Text: fmt.Sprintf(
			"I see you pointed me at **%s** on GitHub, but auto-fetch is disabled in this build. `git clone %s ~/some/path` and ask me to scan that path.",
			intent.GithubURL, intent.GithubURL,
		),
		Target:    intent.GithubRepo,
		GithubURL: intent.GithubURL,
	}
}

// truncateForDisplay shortens long error messages so chat bubbles stay
// readable. The duplicate-named helper in status.go is for a different
// concern (status messages) so we keep two with distinct names rather than
// hoist into a shared util module.
func truncateForDisplay(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func scanReply(_ AssistantDeps, resolver *assistant.Resolver, conv *assistant.Conversation, target string) AssistantReply {
	cands, err := resolver.Resolve(target)
	if err != nil {
		return AssistantReply{
			Kind: "error",
			Text: "I had trouble looking up that name: " + err.Error(),
		}
	}
	conv.LastQueriedTarget = target
	if len(cands) == 0 {
		conv.PendingCandidates = nil
		// Soft "did you mean?" — Levenshtein over inventory + marketplaces.
		// Surfaces up to 3 closest matches so the user can correct a typo
		// without re-typing the whole message. UI renders these as chips.
		suggestions := resolver.Suggest(target, 3)
		text := fmt.Sprintf(
			"I couldn't find anything matching **%s** locally. I look at installed plugins and your marketplace cache under `~/.claude/plugins/marketplaces`.",
			target,
		)
		if len(suggestions) > 0 {
			var names []string
			for _, s := range suggestions {
				names = append(names, "**"+s.Name+"**")
			}
			text += " Did you mean " + strings.Join(names, ", ") + "?"
		} else {
			text += " If it's only on GitHub, GitHub fetch ships in v0.6 — install it locally first or pass me a custom path."
		}
		return AssistantReply{
			Kind:        "text",
			Text:        text,
			Suggestions: suggestions,
		}
	}
	conv.PendingCandidates = cands
	if len(cands) == 1 {
		c := cands[0]
		text := fmt.Sprintf(
			"Found one match: **%s**%s under `%s`. Want me to scan it?",
			c.Name, versionSuffix(c.Version), c.LocalPath,
		)
		return AssistantReply{
			Kind:       "proposal",
			Text:       text,
			Target:     target,
			Candidates: cands,
		}
	}
	return AssistantReply{
		Kind: "proposal",
		Text: fmt.Sprintf(
			"I found %d matches for %q. Pick one and I'll start the scan.",
			len(cands), target,
		),
		Target:     target,
		Candidates: cands,
	}
}

func confirmReply(d AssistantDeps, conv *assistant.Conversation, index int) AssistantReply {
	if len(conv.PendingCandidates) == 0 {
		return AssistantReply{
			Kind: "text",
			Text: "I don't have a candidate queued — tell me which plugin or MCP server to scan, e.g. \"check vercel\" or \"is firecrawl safe?\".",
		}
	}
	if index < 0 || index >= len(conv.PendingCandidates) {
		return AssistantReply{
			Kind: "text",
			Text: fmt.Sprintf("I only have %d candidate(s) queued — pick a number between 1 and %d.", len(conv.PendingCandidates), len(conv.PendingCandidates)),
		}
	}
	c := conv.PendingCandidates[index]
	scanID, err := startScan(d, c.LocalPath)
	if err != nil {
		return AssistantReply{
			Kind: "error",
			Text: "I couldn't start the scan: " + err.Error(),
		}
	}
	// Once a scan starts, clear the pending list so a second "yes" doesn't
	// re-queue the same scan.
	conv.PendingCandidates = nil
	return AssistantReply{
		Kind:   "scan_started",
		Text:   fmt.Sprintf("Scanning **%s** now — I'll narrate every stage below.", c.Name),
		Target: c.Name,
		ScanID: scanID,
	}
}

func startScan(d AssistantDeps, target string) (string, error) {
	if d.StartScan == nil || d.Runner == nil {
		return "", fmt.Errorf("scan execution not wired")
	}
	cleanedTarget := target
	if len(d.AllowedRoots) > 0 {
		validated, err := EnsureAllowed(target, d.AllowedRoots)
		if err != nil {
			return "", fmt.Errorf("%w: candidate path is outside the configured scan roots", ErrPathNotAllowed)
		}
		cleanedTarget = validated
	}
	scanID := uuid.NewString()
	d.Runner.Register(scanID)
	go d.StartScan(context.Background(), scanID, cleanedTarget, true /* offline */, "" /* no auto-diff */, d.Runner)
	return scanID, nil
}

func listReply(d AssistantDeps) AssistantReply {
	if d.LoadInventory == nil {
		return AssistantReply{Kind: "text", Text: "Inventory isn't available right now."}
	}
	inv, err := d.LoadInventory()
	if err != nil {
		return AssistantReply{Kind: "error", Text: "Couldn't read inventory: " + err.Error()}
	}
	if len(inv.Items) == 0 {
		return AssistantReply{Kind: "text", Text: "I don't see any installed plugins or MCP servers in your inventory yet."}
	}
	var b strings.Builder
	b.WriteString("Here's what I see on this machine. Tell me which one to check:\n\n")
	count := 0
	for _, it := range inv.Items {
		if it.LocalPath == "" {
			continue
		}
		count++
		if it.Version != "" {
			fmt.Fprintf(&b, "1. **%s** v%s — `%s`\n", it.Name, it.Version, it.Kind)
		} else {
			fmt.Fprintf(&b, "1. **%s** — `%s`\n", it.Name, it.Kind)
		}
		if count >= 20 {
			fmt.Fprintf(&b, "\n…and more. Ask for any specific one by name.")
			break
		}
	}
	return AssistantReply{Kind: "text", Text: b.String()}
}

func helpText() string {
	return strings.Join([]string{
		"I'm Assay — I run security scans on your Claude Code plugins and MCP servers. Some things you can ask me:",
		"",
		"1. **\"check vercel\"** or **\"is firecrawl safe?\"** — I look up the named plugin and offer to scan it.",
		"2. **\"list plugins\"** — I show what I can see on this machine.",
		"3. **\"yes\"** / **\"scan the second one\"** — after I propose candidates, you tell me which to run.",
		"4. **\"no\"** / **\"cancel\"** — drop the proposal.",
		"",
		"For v0.5 I only look at plugins already installed on this machine, plus your marketplace cache. GitHub fetch ships in v0.6.",
	}, "\n")
}

func didNotUnderstand(text string) string {
	preview := text
	if len(preview) > 60 {
		preview = preview[:60] + "…"
	}
	return fmt.Sprintf(
		"I didn't recognise that as a plugin lookup. Try \"check <name>\" or \"is <name> safe?\" — or ask \"help\" to see what I can do.\n\n(your message: \"%s\")",
		preview,
	)
}

func versionSuffix(v string) string {
	if v == "" {
		return ""
	}
	return " v" + v
}
