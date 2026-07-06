#!/bin/sh
# Assay installer
#   Usage:  curl -sSL https://github.com/chawdamrunal/assay/install | sh
#   Override version:  ASSAY_VERSION=v0.1.0 curl -sSL https://github.com/chawdamrunal/assay/install | sh
#   Override install dir:  ASSAY_INSTALL_DIR=/opt/bin curl -sSL https://github.com/chawdamrunal/assay/install | sh

set -eu

# ---- Configuration ----
REPO="${ASSAY_REPO:-chawdamrunal/assay}"
VERSION="${ASSAY_VERSION:-}"
INSTALL_DIR="${ASSAY_INSTALL_DIR:-}"

# ---- Helpers ----
err() { printf 'assay-install: error: %s\n' "$1" >&2; exit 1; }
info() { printf 'assay-install: %s\n' "$1"; }
have() { command -v "$1" >/dev/null 2>&1; }

# ---- OS / arch detection ----
detect_os() {
  case "$(uname -s)" in
    Darwin) echo darwin ;;
    Linux) echo linux ;;
    *) err "unsupported OS: $(uname -s) - for Windows, see https://github.com/$REPO/releases" ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo amd64 ;;
    arm64|aarch64) echo arm64 ;;
    *) err "unsupported architecture: $(uname -m)" ;;
  esac
}

# ---- Dependency check ----
need_curl_or_wget() {
  if have curl; then
    DL="curl -fsSL -o"
    DL_TO_STDOUT="curl -fsSL"
  elif have wget; then
    DL="wget -qO"
    DL_TO_STDOUT="wget -qO-"
  else
    err "neither curl nor wget found - install one and retry"
  fi
}

need_tar() { have tar || err "tar not found"; }
need_sha256() {
  if have shasum; then
    SHA256="shasum -a 256"
  elif have sha256sum; then
    SHA256="sha256sum"
  else
    err "no shasum or sha256sum command found"
  fi
}

# ---- Version resolution ----
latest_version() {
  $DL_TO_STDOUT "https://api.github.com/repos/$REPO/releases/latest" 2>/dev/null \
    | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' \
    | head -n 1
}

# ---- Install dir resolution ----
resolve_install_dir() {
  if [ -n "$INSTALL_DIR" ]; then
    echo "$INSTALL_DIR"
    return
  fi
  # Prefer /usr/local/bin if writable; else ~/.local/bin
  if [ -w /usr/local/bin ] || { have sudo && [ "$(id -u)" -ne 0 ]; }; then
    echo /usr/local/bin
  else
    echo "$HOME/.local/bin"
  fi
}

# ---- Main ----
main() {
  need_curl_or_wget
  need_tar
  need_sha256

  os=$(detect_os)
  arch=$(detect_arch)

  if [ -z "$VERSION" ]; then
    VERSION=$(latest_version)
    [ -z "$VERSION" ] && err "could not determine latest version (set ASSAY_VERSION=v0.X.Y to override)"
  fi
  # Strip leading 'v' for the tarball name (goreleaser convention)
  version_num="${VERSION#v}"

  tarball="assay_${version_num}_${os}_${arch}.tar.gz"
  url="https://github.com/$REPO/releases/download/$VERSION/$tarball"
  sums_url="https://github.com/$REPO/releases/download/$VERSION/checksums.txt"

  install_dir=$(resolve_install_dir)
  info "version: $VERSION"
  info "platform: $os/$arch"
  info "install dir: $install_dir"

  # Work in a temp dir
  tmpdir=$(mktemp -d 2>/dev/null || mktemp -d -t assay)
  trap 'rm -rf "$tmpdir"' EXIT

  info "downloading $tarball"
  $DL "$tmpdir/$tarball" "$url" || err "download failed: $url"

  info "verifying checksum"
  $DL "$tmpdir/checksums.txt" "$sums_url" || err "checksum file download failed"
  expected_line=$(grep " $tarball\$" "$tmpdir/checksums.txt" || true)
  [ -z "$expected_line" ] && err "no checksum entry for $tarball in checksums.txt"
  expected=$(echo "$expected_line" | awk '{print $1}')
  actual=$( ( cd "$tmpdir" && $SHA256 "$tarball" | awk '{print $1}' ) )
  [ "$expected" = "$actual" ] || err "checksum mismatch (expected $expected, got $actual)"

  info "extracting"
  tar -xzf "$tmpdir/$tarball" -C "$tmpdir"

  # Install
  mkdir -p "$install_dir"
  if [ -w "$install_dir" ]; then
    install -m 0755 "$tmpdir/assay" "$install_dir/assay"
  elif have sudo; then
    info "writing to $install_dir requires sudo"
    sudo install -m 0755 "$tmpdir/assay" "$install_dir/assay"
  else
    err "cannot write to $install_dir and sudo is not available - set ASSAY_INSTALL_DIR to a writable path"
  fi

  info "installed: $install_dir/assay"

  # PATH hint
  case ":$PATH:" in
    *":$install_dir:"*) : ;;
    *) info "NOTE: $install_dir is not in your PATH - add it to your shell profile:"
       info "  export PATH=\"$install_dir:\$PATH\""
       ;;
  esac

  echo
  echo "Next steps:"
  echo "  assay auth status       # see which credential method is active"
  echo "  assay inventory         # list installed plugins / MCP servers / hooks"
  echo "  assay scan <target>     # run a 5-stage security scan"
  echo
  echo "Read the threat model:  https://github.com/$REPO/blob/main/docs/threat-model-2026.md"
}

main "$@"
