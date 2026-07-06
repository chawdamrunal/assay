package assistant

import "testing"

func TestParseIntent(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want Intent
	}{
		{
			name: "scan verb + plugin",
			in:   "check vercel",
			want: Intent{Action: ActionScan, Target: "vercel"},
		},
		{
			name: "is X safe",
			in:   "is firecrawl safe?",
			want: Intent{Action: ActionScan, Target: "firecrawl"},
		},
		{
			name: "is the X plugin safe",
			in:   "is the foo-bar plugin safe to install?",
			want: Intent{Action: ActionScan, Target: "foo-bar"},
		},
		{
			name: "audit verb",
			in:   "audit the playwright plugin please",
			want: Intent{Action: ActionScan, Target: "playwright"},
		},
		{
			name: "pure yes",
			in:   "yes",
			want: Intent{Action: ActionConfirm, Index: 0},
		},
		{
			name: "pure yes with emoji",
			in:   "👍",
			want: Intent{Action: ActionConfirm, Index: 0},
		},
		{
			name: "pure no",
			in:   "nevermind",
			want: Intent{Action: ActionDeny, Index: 0},
		},
		{
			name: "scan ordinal second",
			in:   "scan the second one",
			want: Intent{Action: ActionConfirm, Index: 1},
		},
		{
			name: "do the third",
			in:   "do the third",
			want: Intent{Action: ActionConfirm, Index: 2},
		},
		{
			name: "pick 2",
			in:   "pick 2",
			want: Intent{Action: ActionConfirm, Index: 1},
		},
		{
			name: "list plugins",
			in:   "list my plugins",
			want: Intent{Action: ActionList},
		},
		{
			name: "show plugins",
			in:   "show all plugins",
			want: Intent{Action: ActionList},
		},
		{
			name: "help",
			in:   "what can you do?",
			want: Intent{Action: ActionHelp},
		},
		{
			name: "github short form",
			in:   "scan vercel/next.js",
			want: Intent{
				Action: ActionScanGitHub, Target: "next.js",
				GithubOwner: "vercel", GithubRepo: "next.js",
				GithubURL: "https://github.com/vercel/next.js",
			},
		},
		{
			name: "github full URL",
			in:   "check https://github.com/foo/bar please",
			want: Intent{
				Action: ActionScanGitHub, Target: "bar",
				GithubOwner: "foo", GithubRepo: "bar",
				GithubURL: "https://github.com/foo/bar",
			},
		},
		{
			name: "github URL with hyphenated repo (modelcontextprotocol case)",
			in:   "check https://github.com/modelcontextprotocol/servers",
			want: Intent{
				Action: ActionScanGitHub, Target: "servers",
				GithubOwner: "modelcontextprotocol", GithubRepo: "servers",
				GithubURL: "https://github.com/modelcontextprotocol/servers",
			},
		},
		{
			name: "github full URL with .git suffix (clone-style URL)",
			in:   "scan https://github.com/chawdamrunal/assay.git",
			want: Intent{
				Action: ActionScanGitHub, Target: "assay",
				GithubOwner: "chawdamrunal", GithubRepo: "assay",
				GithubURL: "https://github.com/chawdamrunal/assay",
			},
		},
		{
			name: "github short form with .git suffix",
			in:   "scan chawdamrunal/assay.git",
			want: Intent{
				Action: ActionScanGitHub, Target: "assay",
				GithubOwner: "chawdamrunal", GithubRepo: "assay",
				GithubURL: "https://github.com/chawdamrunal/assay",
			},
		},
		{
			name: "shortform yes/no must NOT be a repo ref",
			in:   "yes/no",
			want: Intent{Action: ActionUnknown},
		},
		{
			name: "shortform on/off must NOT be a repo ref",
			in:   "on/off",
			want: Intent{Action: ActionUnknown},
		},
		{
			name: "shortform fraction must NOT be a repo ref",
			in:   "1/2 done",
			want: Intent{Action: ActionUnknown},
		},
		{
			name: "empty",
			in:   "",
			want: Intent{Action: ActionUnknown},
		},
		{
			name: "garbage",
			in:   "what is the weather today",
			want: Intent{Action: ActionUnknown},
		},
		{
			name: "reserved word filter",
			in:   "is the plugin safe?",
			want: Intent{Action: ActionUnknown},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ParseIntent(c.in)
			if got != c.want {
				t.Fatalf("ParseIntent(%q) = %#v, want %#v", c.in, got, c.want)
			}
		})
	}
}
