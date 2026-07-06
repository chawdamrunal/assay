#!/usr/bin/env bash
#
# Assay pre-install gate — v0.4.
#
# Wired as a UserPromptSubmit hook in Claude Code's settings.json by
# `assay hook install`. Fires on every user prompt; fast-exits in <2 ms when
# the prompt is not `/plugin install <ref>`.
#
# When it IS a plugin-install command:
#   1. Resolve the plugin source on disk (~/.claude/plugins/marketplaces/...)
#   2. Run `assay scan --quick --json` (no LLM call, deterministic pre-pass)
#   3. Map risk → permissionDecision:
#        critical/high → deny
#        medium        → ask
#        low/unknown   → allow (with additionalContext nudging a deep scan)
#
# Graceful degradation is the rule: any infrastructure failure exits 0 with
# no output, so a broken assay install never blocks the user's session.
# Hard-coded "fail open" — Assay is informational, not a security barrier.

set -uo pipefail

# --- Helpers -----------------------------------------------------------------

emit_allow_with_context() {
  local ref="$1" risk="$2" deep="$3"
  local ctx="Assay pre-install gate: $ref risk=$risk."
  if [[ -n "$deep" ]]; then
    ctx="$ctx Deep scan running in background as $deep — open http://localhost:7373/scans/$deep when assay serve is running."
  fi
  printf '{"hookSpecificOutput":{"hookEventName":"UserPromptSubmit","additionalContext":%s}}\n' \
    "$(printf '%s' "$ctx" | python3 -c 'import sys,json;print(json.dumps(sys.stdin.read()))')"
}

emit_ask() {
  local ref="$1" summary="$2"
  printf '{"hookSpecificOutput":{"hookEventName":"UserPromptSubmit","permissionDecision":"ask","permissionDecisionReason":%s}}\n' \
    "$(printf 'Assay pre-install gate: %s — %s. Continue with install?' "$ref" "$summary" | python3 -c 'import sys,json;print(json.dumps(sys.stdin.read()))')"
}

emit_deny() {
  local ref="$1" summary="$2"
  printf '{"hookSpecificOutput":{"hookEventName":"UserPromptSubmit","permissionDecision":"deny","permissionDecisionReason":%s}}\n' \
    "$(printf 'Assay pre-install gate BLOCKED %s: %s. Inspect with `assay scan %s` before retrying.' "$ref" "$summary" "$ref" | python3 -c 'import sys,json;print(json.dumps(sys.stdin.read()))')"
}

# Portable bounded execution. Stock macOS has no `timeout` binary, so the old
# `timeout 25 ...` errored, `|| true` swallowed it, and the quick scan never
# ran — the gate was a silent no-op on macOS. Prefer GNU `timeout`, then
# `gtimeout` (brew coreutils), else run uncapped rather than skip the scan.
run_with_timeout() {
  local secs="$1"; shift
  if command -v timeout >/dev/null 2>&1; then
    timeout "$secs" "$@"
  elif command -v gtimeout >/dev/null 2>&1; then
    gtimeout "$secs" "$@"
  else
    "$@"
  fi
}

# --- Read stdin --------------------------------------------------------------

input=$(cat)

# The gate parses its JSON payload with python3. If python3 is missing we
# cannot proceed; fail open, but say so on stderr when this is actually a
# plugin install (so the no-op is visible, not silent — the previous behavior
# left users wondering why the gate never fired).
if ! command -v python3 >/dev/null 2>&1; then
  case "$input" in
    *"/plugin install"*) echo "assay-pre-install: python3 not found on PATH; gate disabled (fail-open)" >&2 ;;
  esac
  exit 0
fi
prompt=$(printf '%s' "$input" | python3 -c 'import sys,json;d=json.loads(sys.stdin.read() or "{}");print(d.get("user_prompt") or d.get("userPrompt") or "")' 2>/dev/null)

# Fast exit on anything that isn't a plugin install.
case "$prompt" in
  *"/plugin install"*) ;;
  *) exit 0 ;;
esac

# Extract "<ref>" — the first non-flag token after "install".
ref=$(printf '%s' "$prompt" | awk '/\/plugin install/{
  for (i=1; i<=NF; i++) if ($i == "install") { print $(i+1); exit }
}')
ref="${ref#./}"
if [[ -z "$ref" ]]; then
  exit 0
fi

# Locate assay on PATH; fall back to a couple of common install dirs.
ASSAY_BIN=$(command -v assay 2>/dev/null || true)
if [[ -z "$ASSAY_BIN" ]]; then
  for candidate in "$HOME/.local/bin/assay" "/usr/local/bin/assay" "/opt/homebrew/bin/assay"; do
    if [[ -x "$candidate" ]]; then ASSAY_BIN="$candidate"; break; fi
  done
fi
if [[ -z "$ASSAY_BIN" ]]; then
  # No assay installed — fail open silently.
  exit 0
fi

# Resolve the plugin source on disk.
src=$("$ASSAY_BIN" hook resolve "$ref" 2>/dev/null || true)
if [[ -z "$src" || ! -d "$src" ]]; then
  # Unknown reference (typo, private marketplace not added, etc) — let the
  # user proceed; Claude Code's own install logic will surface the error.
  exit 0
fi

# Run the quick scan with a hard wall-clock timeout.
quick_json=$(run_with_timeout 25 "$ASSAY_BIN" scan --quick --json --spawn-deep "$src" 2>/dev/null || true)
if [[ -z "$quick_json" ]]; then
  exit 0
fi

risk=$(printf '%s' "$quick_json" | python3 -c 'import sys,json;d=json.loads(sys.stdin.read() or "{}");print(d.get("risk") or "unknown")' 2>/dev/null)
summary=$(printf '%s' "$quick_json" | python3 -c '
import sys,json
d=json.loads(sys.stdin.read() or "{}")
c=d.get("counts",{})
parts=[]
for k in ("critical","high","medium"):
  v=c.get(k,0)
  if v>0: parts.append(f"{v} {k}")
print(", ".join(parts) or "no pre-pass hits")' 2>/dev/null)
deep_id=$(printf '%s' "$quick_json" | python3 -c 'import sys,json;d=json.loads(sys.stdin.read() or "{}");print(d.get("deep_scan_id") or "")' 2>/dev/null)

case "$risk" in
  critical|high) emit_deny "$ref" "$summary" ;;
  medium)        emit_ask  "$ref" "$summary" ;;
  *)             emit_allow_with_context "$ref" "$risk" "$deep_id" ;;
esac
exit 0
