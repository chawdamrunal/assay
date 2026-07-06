# Installing Assay

Assay ships as **one self-contained binary**. The Go executable has the entire React web UI embedded inside it (via Go's `embed.FS`), so there is nothing to deploy alongside it — no Node.js at runtime, no separate web server, no database. The single `bin/assay` file *is* the CLI, the local web server, and the MCP server; you pick which role it plays with a subcommand (`assay serve`, `assay mcp`, `assay scan`, …).

There are two ways to install it:

1. **Build from source** — works today, straight from a clone of this repo. Use this now: the project is pre-1.0 and no tagged release has been published yet, so this is the only path that currently produces a binary.
2. **Prebuilt release channels** — the `curl | sh` and PowerShell installers, Homebrew, WinGet, Docker, and the Claude Code plugin. These are wired up (`install.sh`, `install.ps1`, `.goreleaser.yaml`) but only become live once a `v*.*.*` release is tagged. They are documented in section 5 so they are ready the moment a release ships.

---

## 1. Prerequisites

To **build from source** you need:

1. **Go 1.25.5 or newer** — the build is pinned to `go 1.25.5` in `go.mod`. Check with `go version`.
2. **Node.js 22+** — the version CI and the Docker build use (Node 20.19+ will likely work for Vite 7 / React 19, but 22 is the tested floor). Check with `node --version`.
3. **pnpm 10+** — the web package manager. The easiest way to get the right version is Corepack, which ships with Node: `corepack enable && corepack prepare pnpm@10 --activate`. Check with `pnpm --version`.
4. **git** and **make** — to clone and drive the build.

To **run scans in the default (MCP) mode** you also need:

5. **Claude Code CLI** (`claude`) on your `PATH` — the default scan mode drives your Claude Code subscription, so the reasoning runs on your existing login with no extra API key. Check with `claude --version`. (Only the legacy `--scan-mode legacy` path needs an `ANTHROPIC_API_KEY` instead; see section 6.)

A C compiler is **not** required — the binary is built with `CGO_ENABLED=0`.

---

## 2. Build from source

### 2.1 Clone the repository

```bash
git clone https://github.com/chawdamrunal/assay.git
cd assay
```

### 2.2 Build the binary

```bash
make build
```

What `make build` does, in order:

1. `cd web && pnpm install --frozen-lockfile && pnpm build` — installs the exact locked frontend dependencies and compiles the React SPA with Vite into `web/dist/`.
2. Copies `web/dist/` into `internal/api/dist/` — the directory Go embeds at compile time.
3. `verify-embed` — asserts `internal/api/dist/index.html` actually landed, so a silent copy failure can't ship a binary with no UI.
4. `go build -o bin/assay ./cmd/assay` — compiles the Go binary with the UI baked in.

When it finishes you get a single file at `bin/assay` (and the target prints its size and absolute path).

**If your Go is not the Homebrew build:** the Makefile defaults `GO` to the Homebrew path (`/opt/homebrew/opt/go/bin/go`). On any other setup, point it at your Go on the command line — command-line values override the Makefile:

```bash
make build GO="$(command -v go)"
```

### 2.3 Run it in place

```bash
./bin/assay version
./bin/assay inventory
```

### 2.4 Install it onto your PATH

```bash
make install
```

`make install` builds (if needed) and copies the binary to `$PREFIX/bin/assay`, defaulting to `~/.local/bin/assay`. Override the location for a system-wide install:

```bash
make install PREFIX=/usr/local      # installs to /usr/local/bin/assay (may need sudo)
```

If the install directory isn't already on your `PATH`, add it to your shell profile:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

Confirm it resolves globally:

```bash
assay version
```

---

## 3. First run

### 3.1 Inventory what's installed

See every Claude Code plugin, MCP server (read from `~/.claude.json` and Codex's `~/.codex/config.toml`), hook, skill, connector, and settings override on this machine:

```bash
assay inventory          # human-readable table
assay inventory --json   # machine-readable
```

### 3.2 Launch the web UI

```bash
assay serve
```

Then open **http://localhost:7373**. Click **New Scan**, pick a target, and the scan runs by spawning `claude -p` with the Assay MCP server wired in — it uses your Claude Code subscription quota, so there are no API rate-limit problems.

Scan-mode variants:

1. `assay serve` — default; scans via your Claude Code subscription (MCP mode).
2. `assay serve --scan-mode legacy` — in-process Go orchestrator calling the Anthropic API directly (needs an API key; for CI / no-Claude-Code setups).
3. `assay serve --scan-mode fake` — replays recorded fixtures, no LLM call (demos / offline dev).

### 3.3 Use Assay from inside Claude Code (MCP)

Register Assay as an MCP server so you can run `/assay-scan <target>` in a Claude Code session. The authoritative form is an entry in your user-scoped `~/.claude.json` under `mcpServers` (point `command` at the binary you installed):

```json
{
  "mcpServers": {
    "assay": {
      "type": "stdio",
      "command": "/Users/you/.local/bin/assay",
      "args": ["mcp", "--transport", "stdio"]
    }
  }
}
```

You can also add it with the Claude Code CLI — see `claude mcp --help` for the exact `claude mcp add` syntax on your version.

### 3.4 Scan from the CLI

```bash
# Full 5-stage scan of a plugin / MCP server / local directory
assay scan ~/.claude/plugins/cache/<marketplace>/<plugin>/<version>

# Scan every installed plugin and skill, aggregated
assay scan-all --parallel 2

# Fast deterministic pre-pass only (no LLM, <2s; used by the install gate)
assay scan --quick --json ~/.claude/plugins/cache/<m>/<plugin>/<version>
```

### 3.5 Install the pre-install gate (optional)

```bash
assay hook install
```

This adds a hook to `~/.claude/settings.json` so any `/plugin install` in Claude Code is scanned *before* it commits: deny on critical/high, ask on medium, allow (with a deep background scan) on low. Remove it with `assay hook uninstall`.

---

## 4. Build and run with Docker

The repo includes a multi-stage `Dockerfile` that builds the frontend, compiles a static Go binary, and ships a `scratch` image. Build it from source today:

```bash
docker build -t assay:local .
docker run --rm assay:local version
```

Mount a directory to scan it (legacy/API-key mode inside a container, since `claude` isn't present in the image):

```bash
docker run --rm -v ~/.claude:/scan assay:local scan /scan
```

The container exposes port `7373` for `assay serve`.

---

## 5. Prebuilt release channels (live after a release is tagged)

These are configured (`install.sh`, `install.ps1`, `.goreleaser.yaml` with `brews:` + `winget:` + `dockers:`) and will work as soon as the first `v*.*.*` tag is pushed and GoReleaser publishes the GitHub Release, Homebrew formula, WinGet manifest, and `ghcr.io` image. Until then, use **build from source** (section 2).

### 5.1 Install script (macOS / Linux / WSL)

```bash
curl -sSL https://github.com/chawdamrunal/assay/install | sh
```

It detects your OS/arch, downloads the matching `assay_<version>_<os>_<arch>.tar.gz` from the GitHub Release, **verifies the SHA-256 checksum**, and installs to `/usr/local/bin` (or `~/.local/bin` if that isn't writable). Override with environment variables:

```bash
ASSAY_VERSION=v0.1.0 ASSAY_INSTALL_DIR=/opt/bin curl -sSL https://github.com/chawdamrunal/assay/install | sh
```

### 5.2 Install script (Windows / PowerShell)

```powershell
irm https://github.com/chawdamrunal/assay/install.ps1 | iex
```

It detects your architecture, downloads `assay_<version>_windows_amd64.zip` from the GitHub Release, **verifies the SHA-256 checksum**, extracts `assay.exe` to `%LOCALAPPDATA%\Assay\bin`, and adds that directory to your **user `PATH`** — no administrator rights required (restart your terminal afterwards). Override with environment variables:

```powershell
$env:ASSAY_VERSION = 'v0.1.0'; $env:ASSAY_INSTALL_DIR = 'C:\tools\assay'; irm https://github.com/chawdamrunal/assay/install.ps1 | iex
```

Windows arm64 installs the amd64 build (it runs under emulation); 32-bit x86 is not supported.

### 5.3 WinGet

```powershell
winget install AssaySec.Assay
```

The release pipeline opens a manifest PR to [microsoft/winget-pkgs](https://github.com/microsoft/winget-pkgs) (`.goreleaser.yaml` → `winget:`). `winget install` works once that PR is merged for the release — which requires the GitHub Release to be **published** (not a draft) so Microsoft's validation can download the installer.

### 5.4 Homebrew

```bash
brew install chawdamrunal/tap/assay
```

### 5.5 Docker (published image)

```bash
docker run --rm -v ~/.claude:/scan ghcr.io/chawdamrunal/assay:latest scan /scan
```

### 5.6 Claude Code plugin

```
/plugin install chawdamrunal/assay
```

### 5.7 Manual download

Grab the tarball for your platform from the [GitHub Releases](https://github.com/chawdamrunal/assay/releases) page, verify it against `checksums.txt`, extract, and move `assay` onto your `PATH`.

---

## 6. Authentication

For **default (MCP) mode** there is nothing to configure — scans run through your existing Claude Code login.

For **legacy mode** (`--scan-mode legacy` or `assay scan` driving the Anthropic API directly), Assay resolves credentials in priority order:

1. The `ANTHROPIC_API_KEY` environment variable.
2. A key stored in the OS keychain via `assay config set api-key sk-ant-...`.
3. Your existing Claude Code OAuth credentials (auto-detected), with built-in 429 retry and exponential backoff.

Check which method is active:

```bash
assay auth status
```

---

## 7. Verify the install

```bash
assay version            # prints version / commit / build date
assay inventory          # should list your plugins, MCP servers, hooks
assay serve              # then open http://localhost:7373
```

---

## 8. Development workflow

```bash
make test     # go test -race ./...  +  web lint
make lint     # golangci-lint  +  web lint
make clean    # remove bin/ and the embedded dist (keeps node_modules)
```

To iterate on the frontend with hot-reload (separate from the embedded build):

```bash
cd web && pnpm dev        # Vite dev server
cd web && pnpm typecheck  # TypeScript check only
```

`make test` is the gate: it runs the full Go race suite plus the web lint. `golangci-lint` is required for `make lint` and CI; install it separately if it isn't already on your machine.

---

## 9. Uninstall

```bash
assay hook uninstall              # remove the pre-install gate, if you installed it
rm -f ~/.local/bin/assay          # or wherever you installed it (see `command -v assay`)
rm -rf ~/.assay                   # scan history, config, and cached state
```

If you registered the MCP server, remove the `assay` entry from your `~/.claude.json` `mcpServers` block.
