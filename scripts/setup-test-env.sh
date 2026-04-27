#!/usr/bin/env bash
# Spin up an isolated tower sandbox for manual TUI testing on Windows
# (Git Bash / MSYS / WSL).
#
# - Builds tower.exe (idempotent)
# - Builds and runs scripts/seed which creates ~6 fake repos with a
#   handful of worktrees each, in varied state (clean, dirty, ahead).
# - Points APPDATA at a sandbox dir so tower's state.db is isolated
#   from your real tower state.
# - Prints the exact command to launch the TUI.
#
# Re-running the script wipes the sandbox and starts over.

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
SANDBOX="$ROOT_DIR/.sandbox"
REPOS_DIR="$SANDBOX/repos"
STATE="$SANDBOX/state"

echo "==> sandbox at $SANDBOX"
rm -rf "$SANDBOX"
mkdir -p "$REPOS_DIR" "$STATE"

echo "==> building tower.exe"
( cd "$ROOT_DIR" && go build -o tower.exe ./cmd/tower )

echo "==> seeding playground"
( cd "$ROOT_DIR" && go run ./scripts/seed -root "$REPOS_DIR" -state "$STATE" )

echo "==> sandbox ready."
echo
echo "    sandbox APPDATA: $STATE"
echo "    fake repos:      $REPOS_DIR"
echo
echo "Launch the TUI with:"
echo
if command -v cygpath >/dev/null 2>&1; then
  WIN_STATE=$(cygpath -w "$STATE")
  WIN_BIN=$(cygpath -w "$ROOT_DIR/tower.exe")
else
  WIN_STATE="$STATE"
  WIN_BIN="$ROOT_DIR/tower.exe"
fi
echo "  bash:        APPDATA='$STATE' '$ROOT_DIR/tower.exe'"
echo "  PowerShell:  \$env:APPDATA='$WIN_STATE'; & '$WIN_BIN'"
echo
echo "  Add TOWER_DEBUG=1 to either to write trace logs to %TEMP%/tower-debug.log."
echo
echo "Press q to quit the TUI; the sandbox files stay until you re-run this script."
