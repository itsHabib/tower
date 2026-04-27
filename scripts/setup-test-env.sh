#!/usr/bin/env bash
# Spin up an isolated tower sandbox for manual TUI testing on Windows
# (Git Bash / MSYS / WSL).
#
# - Builds tower.exe (idempotent)
# - Creates a fresh git repo with a few commits and a couple of
#   throwaway tower-style worktrees so the board has rows to play with.
# - Points APPDATA at a sandbox dir so tower's state.db is isolated
#   from your real tower state.
# - Prints the exact command to launch the TUI.
#
# Re-running the script wipes the sandbox and starts over.

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
SANDBOX="$ROOT_DIR/.sandbox"
REPO="$SANDBOX/repo"
STATE="$SANDBOX/state"

echo "==> sandbox at $SANDBOX"
rm -rf "$SANDBOX"
mkdir -p "$REPO" "$STATE"

echo "==> building tower.exe"
( cd "$ROOT_DIR" && go build -o tower.exe ./cmd/tower )

echo "==> initializing throwaway git repo"
(
  cd "$REPO"
  git init -q
  git config user.email sandbox@tower
  git config user.name sandbox
  echo "# sandbox repo" > README.md
  git add README.md
  git commit -qm "initial"
  echo "feat 1" > a.txt
  git add a.txt
  git commit -qm "add a"
  echo "feat 2" > b.txt
  git add b.txt
  git commit -qm "add b"
)

echo "==> registering repo and seeding worktrees"
export APPDATA="$STATE"
"$ROOT_DIR/tower.exe" repo add "$REPO" >/dev/null
"$ROOT_DIR/tower.exe" add --repo repo feat-x >/dev/null
"$ROOT_DIR/tower.exe" add --repo repo feat-y >/dev/null

# Make one worktree dirty and one with an unmerged commit so you can
# actually exercise every branch of the d-flow in the TUI.
echo "dirt" > "$REPO/.worktrees/feat-x/dirty.txt"
(
  cd "$REPO/.worktrees/feat-y"
  git config user.email sandbox@tower
  git config user.name sandbox
  echo "secret" > only-on-y.txt
  git add only-on-y.txt
  git commit -qm "only on tower/feat-y (unmerged)"
)

echo "==> sandbox ready."
echo
echo "    sandbox APPDATA: $STATE"
echo "    sandbox repo:    $REPO"
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
