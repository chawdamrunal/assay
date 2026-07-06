package assistant

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/chawdamrunal/assay/internal/inventory"
)

func TestResolverInstalledMatch(t *testing.T) {
	loader := func() (inventory.Inventory, error) {
		return inventory.Inventory{
			Items: []inventory.Item{
				{
					Name:      "frontend-design",
					Kind:      inventory.KindClaudeCodePlugin,
					Version:   "3a92c028770f",
					LocalPath: "/some/path/frontend-design/3a92",
					Metadata:  map[string]string{"marketplace": "claude-plugins-official"},
				},
				{
					Name:      "vercel",
					Kind:      inventory.KindClaudeCodePlugin,
					Version:   "0.42.1",
					LocalPath: "/some/path/vercel/0.42.1",
					Metadata:  map[string]string{"marketplace": "claude-plugins-official"},
				},
				{
					Name:      "firecrawl",
					Kind:      inventory.KindMCPServer,
					LocalPath: "/some/mcp/firecrawl",
				},
			},
		}, nil
	}
	r := &Resolver{LoadInventory: loader}

	t.Run("exact match returns one installed-plugin candidate", func(t *testing.T) {
		got, err := r.Resolve("vercel")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 {
			t.Fatalf("want 1 candidate, got %d: %#v", len(got), got)
		}
		if got[0].Name != "vercel" || got[0].Kind != "installed-plugin" {
			t.Fatalf("unexpected: %#v", got[0])
		}
		if got[0].LocalPath != "/some/path/vercel/0.42.1" {
			t.Fatalf("local path: %q", got[0].LocalPath)
		}
	})

	t.Run("substring match returns installed candidate", func(t *testing.T) {
		got, err := r.Resolve("front")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Name != "frontend-design" {
			t.Fatalf("unexpected: %#v", got)
		}
	})

	t.Run("mcp server matched", func(t *testing.T) {
		got, err := r.Resolve("firecrawl")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Kind != "mcp-server" {
			t.Fatalf("expected mcp-server kind, got %#v", got)
		}
	})

	t.Run("unknown name returns empty", func(t *testing.T) {
		got, err := r.Resolve("definitely-not-installed")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 0 {
			t.Fatalf("want empty, got %#v", got)
		}
	})
}

func TestResolverMarketplaceFallback(t *testing.T) {
	tmp := t.TempDir()
	// Build a marketplaces dir that mirrors the real layout:
	// <tmp>/claude-plugins-official/plugins/cool-tool/plugin.json
	mp := filepath.Join(tmp, "claude-plugins-official", "plugins", "cool-tool")
	if err := os.MkdirAll(mp, 0o750); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"cool-tool","description":"Helps the user be cool","version":"1.2.3"}`
	if err := os.WriteFile(filepath.Join(mp, "plugin.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	r := &Resolver{
		LoadInventory:   func() (inventory.Inventory, error) { return inventory.Inventory{}, nil },
		MarketplacesDir: tmp,
	}
	got, err := r.Resolve("cool-tool")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 candidate, got %d", len(got))
	}
	c := got[0]
	if c.Name != "cool-tool" || c.Kind != "marketplace-plugin" ||
		c.Marketplace != "claude-plugins-official" || c.Version != "1.2.3" ||
		c.Description == "" {
		t.Fatalf("unexpected: %#v", c)
	}
}

func TestResolverInstalledRanksAboveMarketplace(t *testing.T) {
	tmp := t.TempDir()
	mp := filepath.Join(tmp, "official", "plugins", "vercel")
	if err := os.MkdirAll(mp, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mp, "plugin.json"), []byte(`{"name":"vercel","version":"0.42.0"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	loader := func() (inventory.Inventory, error) {
		return inventory.Inventory{
			Items: []inventory.Item{{
				Name: "vercel", Kind: inventory.KindClaudeCodePlugin,
				LocalPath: "/installed/vercel", Version: "0.42.1",
			}},
		}, nil
	}
	r := &Resolver{LoadInventory: loader, MarketplacesDir: tmp}
	got, err := r.Resolve("vercel")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 2 {
		t.Fatalf("want 2+ candidates (installed + marketplace), got %d", len(got))
	}
	if got[0].Kind != "installed-plugin" {
		t.Fatalf("installed should rank first, got %s", got[0].Kind)
	}
}
