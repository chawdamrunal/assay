// Package assistant powers the /assistant chat surface — pattern-matched
// intent extraction over user messages and resolution against the local
// inventory + marketplaces. No LLM is involved at this layer; the goal is
// deterministic behaviour and zero per-message cost.
//
// The contract is intentionally narrow: an Intent is one of a small,
// finite set of actions. Anything we cannot classify becomes ActionUnknown
// so the caller can render a fallback "I didn't understand — try …" reply.
package assistant

import (
	"regexp"
	"strconv"
	"strings"
)

// Action categorises a parsed user message.
type Action string

// Recognised user-message intents.
const (
	ActionUnknown    Action = "unknown"     // no recognised intent
	ActionScan       Action = "scan"        // "check vercel", "is vercel safe?", "scan firecrawl"
	ActionScanGitHub Action = "scan_github" // URL or owner/repo — the source lives on GitHub
	ActionConfirm    Action = "confirm"     // "yes", "do it", "scan the first one"
	ActionDeny       Action = "deny"        // "no", "cancel", "nevermind"
	ActionList       Action = "list"        // "what plugins do I have", "list plugins"
	ActionHelp       Action = "help"        // "help", "what can you do"
)

// Intent is the structured result of ParseIntent.
type Intent struct {
	Action Action
	// Target is the plugin/server name extracted from the message, lowercased.
	// Empty for non-scan actions or when the user references "the first one".
	// For ActionScanGitHub this is the repo name (== GithubRepo) so the
	// local-resolver fallback can still try to find an installed match.
	Target string
	// GithubOwner / GithubRepo carry the parsed GitHub coordinates when
	// Action == ActionScanGitHub. The assistant uses these to compose a
	// message that names the URL the user pasted instead of pretending the
	// reference was a local-only name.
	GithubOwner string
	GithubRepo  string
	GithubURL   string
	// Index is the 0-based candidate selector for "scan the first" /
	// "the second one please". Default 0 (first candidate).
	Index int
}

var (
	reGitHubURL     = regexp.MustCompile(`(?i)https?://github\.com/([a-z0-9][a-z0-9._-]*)/([a-z0-9][a-z0-9._-]*)`)
	reGitHubShort   = regexp.MustCompile(`(?i)\b([a-z0-9][a-z0-9._-]*)/([a-z0-9][a-z0-9._-]*)\b`)
	reScanVerbName  = regexp.MustCompile(`(?i)\b(?:scan|check|audit|review|inspect|analy[sz]e)(?:\s+the)?\s+([a-z0-9][a-z0-9._-]{1,40})\b`)
	reIsSafe        = regexp.MustCompile(`(?i)\bis\s+(?:the\s+)?([a-z0-9][a-z0-9._-]{1,40})\b.*\b(?:safe|ok|okay|trustworthy|secure|good)\b`)
	reNameSafe      = regexp.MustCompile(`(?i)\b([a-z0-9][a-z0-9._-]{1,40})\b.*\b(?:safe|ok|okay|trustworthy|secure|good)\s*\??`)
	reSelectOrdinal = regexp.MustCompile(`(?i)\b(?:scan|do|pick|choose|select|use)\s+(?:the\s+)?(first|second|third|fourth|fifth|1st|2nd|3rd|4th|5th|\d+)\b`)
	rePureYes       = regexp.MustCompile(`(?i)^\s*(?:yes|y|yeah|yep|sure|ok|okay|go|do it|scan it|please|that one|👍)\s*[!.]?\s*$`)
	rePureNo        = regexp.MustCompile(`(?i)^\s*(?:no|n|nope|cancel|never\s*mind|skip|stop)\s*[!.]?\s*$`)
	reList          = regexp.MustCompile(`(?i)\b(?:list|show)\s+(?:my\s+|all\s+)?(?:plugins?|mcps?|servers?|extensions?)\b`)
	reHelp          = regexp.MustCompile(`(?i)\b(?:help|what\s+can\s+you\s+do|how\s+do\s+I|capabilities)\b`)
)

// reservedWords are common English words that look like plugin names but
// aren't. We strip these from candidate-name extraction so "check the source"
// doesn't try to resolve a plugin called "source".
var reservedWords = map[string]bool{
	"the": true, "a": true, "an": true, "is": true, "are": true, "was": true,
	"this": true, "that": true, "it": true, "any": true, "all": true,
	"source": true, "code": true, "plugin": true, "mcp": true, "server": true,
	"safe": true, "ok": true, "good": true, "secure": true, "trustworthy": true,
	"to": true, "use": true, "install": true, "running": true,
	"my": true, "your": true, "please": true,
}

// ParseIntent classifies a raw user message. The function is deterministic
// and tolerant of mild typos in surrounding filler ("could you maybe scan vercel?")
// but does not attempt to correct misspellings in the target name itself.
func ParseIntent(message string) Intent {
	s := strings.TrimSpace(message)
	if s == "" {
		return Intent{Action: ActionUnknown}
	}

	// 1. Pure confirm / deny ("yes" / "no") — must be a near-empty utterance.
	if rePureYes.MatchString(s) {
		return Intent{Action: ActionConfirm}
	}
	if rePureNo.MatchString(s) {
		return Intent{Action: ActionDeny}
	}

	// 2. Ordinal selection: "scan the second one" / "do the first" / "pick 3".
	if m := reSelectOrdinal.FindStringSubmatch(s); m != nil {
		return Intent{Action: ActionConfirm, Index: parseOrdinal(m[1])}
	}

	// 3. List request.
	if reList.MatchString(s) {
		return Intent{Action: ActionList}
	}

	// 4. Help request — only if no plugin name appears (otherwise "help me check vercel" → scan).
	if reHelp.MatchString(s) && reScanVerbName.FindStringIndex(s) == nil {
		return Intent{Action: ActionHelp}
	}

	// 5. GitHub URL — strongest signal; matches before any verb extraction so
	// "scan https://github.com/foo/bar" doesn't get reduced to target="bar".
	if m := reGitHubURL.FindStringSubmatch(s); m != nil {
		owner := strings.ToLower(m[1])
		// Strip a trailing ".git" so a clone-style URL
		// (github.com/owner/repo.git) resolves to repo "repo", not "repo.git"
		// — otherwise the cloner appends another ".git" and the clone 404s.
		// GitHub disallows repo names ending in ".git", so this is always safe.
		repo := strings.TrimSuffix(strings.ToLower(m[2]), ".git")
		return Intent{
			Action:      ActionScanGitHub,
			Target:      repo,
			GithubOwner: owner,
			GithubRepo:  repo,
			GithubURL:   "https://github.com/" + owner + "/" + repo,
		}
	}
	// 5b. Shortform owner/repo — only when at least one segment looks
	// "repo-y" (contains '-', '.', or a digit). This rejects english
	// fragments like "yes/no", "on/off", "1/2" that the old regex
	// happily classified as scan intents (audit Finding #9). It also
	// rejects URL paths via the trailing-slash / scheme checks.
	if m := reGitHubShort.FindStringSubmatch(s); m != nil &&
		!strings.Contains(s, "/api/") && !strings.Contains(s, "://") &&
		looksLikeRepoRef(m[1], m[2]) {
		owner := strings.ToLower(m[1])
		// Strip a trailing ".git" so a clone-style URL
		// (github.com/owner/repo.git) resolves to repo "repo", not "repo.git"
		// — otherwise the cloner appends another ".git" and the clone 404s.
		// GitHub disallows repo names ending in ".git", so this is always safe.
		repo := strings.TrimSuffix(strings.ToLower(m[2]), ".git")
		return Intent{
			Action:      ActionScanGitHub,
			Target:      repo,
			GithubOwner: owner,
			GithubRepo:  repo,
			GithubURL:   "https://github.com/" + owner + "/" + repo,
		}
	}

	// 6. "scan vercel" / "check vercel" / "audit the foo plugin".
	if m := reScanVerbName.FindStringSubmatch(s); m != nil {
		t := normalizeName(m[1])
		if t != "" {
			return Intent{Action: ActionScan, Target: t}
		}
	}

	// 7. "is vercel safe?" / "is the firecrawl plugin trustworthy?"
	if m := reIsSafe.FindStringSubmatch(s); m != nil {
		t := normalizeName(m[1])
		if t != "" {
			return Intent{Action: ActionScan, Target: t}
		}
	}

	// 8. Bare "vercel safe?" — last resort, low confidence.
	if m := reNameSafe.FindStringSubmatch(s); m != nil {
		t := normalizeName(m[1])
		if t != "" {
			return Intent{Action: ActionScan, Target: t}
		}
	}

	return Intent{Action: ActionUnknown}
}

// normalizeName lowercases and rejects reserved English words so we don't
// try to resolve plugins called "the" or "plugin".
func normalizeName(raw string) string {
	t := strings.ToLower(strings.TrimSpace(raw))
	t = strings.TrimSuffix(t, ".")
	t = strings.TrimSuffix(t, ",")
	t = strings.TrimSuffix(t, "?")
	if reservedWords[t] {
		return ""
	}
	if len(t) < 2 {
		return ""
	}
	return t
}

// looksLikeRepoRef returns true when an `owner/repo` shortform looks like a
// real GitHub coordinate rather than an English fragment ("yes/no", "1/2",
// "on/off"). Rules:
//
//  1. Both segments must be at least 2 chars (rejects "1/2", "a/b").
//  2. Neither segment may be a reserved English word (rejects "yes/no",
//     "on/off" — the words live in reservedWords).
//  3. At least one segment must EITHER contain a hyphen / dot / digit OR be
//     6+ characters long. Real repo names tend to satisfy one of these; pairs
//     of short plain-English words rarely do.
func looksLikeRepoRef(owner, repo string) bool {
	for _, part := range []string{owner, repo} {
		if len(part) < 2 {
			return false
		}
		if reservedWords[strings.ToLower(part)] {
			return false
		}
	}
	for _, part := range []string{owner, repo} {
		if len(part) >= 6 {
			return true
		}
		for _, c := range part {
			if c == '-' || c == '.' || (c >= '0' && c <= '9') {
				return true
			}
		}
	}
	return false
}

func parseOrdinal(s string) int {
	switch strings.ToLower(s) {
	case "first", "1st":
		return 0
	case "second", "2nd":
		return 1
	case "third", "3rd":
		return 2
	case "fourth", "4th":
		return 3
	case "fifth", "5th":
		return 4
	}
	if n, err := strconv.Atoi(s); err == nil && n >= 1 {
		return n - 1
	}
	return 0
}
