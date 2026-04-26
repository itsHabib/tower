// Command tower is the CLI entry point for the tower control tower.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/itsHabib/tower/internal/tui"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return openTUI()
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "add":
		return cmdAdd(rest)
	case "rm":
		return cmdRm(rest)
	case "ls":
		return cmdLs(rest)
	case "open":
		return cmdOpen(rest)
	case "sync":
		return cmdSync(rest)
	case "reconcile":
		return cmdReconcile(rest)
	case "repo":
		return cmdRepo(rest)
	case "shell":
		return cmdShell(rest)
	case "help", "-h", "--help":
		printUsage()
		return nil
	default:
		printUsage()
		return fmt.Errorf("unknown command: %s", cmd)
	}
}

func openTUI() error {
	ctx := context.Background()
	c, cleanup, err := setup(ctx)
	if err != nil {
		return err
	}
	defer cleanup()
	return tui.Run(ctx, c.workflow, c.store)
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `tower — manage parallel git worktrees across N repos

usage: tower <command> [args...]

worktree commands:
  add <name>            create a worktree (uses cwd's repo, or --repo)
  rm <name>             tear down a worktree (--repo if name is ambiguous)
  ls                    list all worktrees, grouped by repo
  open <name>           print worktree path (--repo if ambiguous)
  sync                  reconcile from git + refresh PR/CI from GitHub
  reconcile             reconcile from git only (no network)

repo commands:
  repo add [path]       register a repo (defaults to cwd)
  repo ls               list registered repos
  repo rm <name>        unregister a repo

shell integration:
  shell [bash|zsh|powershell]
                        print a shell helper that adds a 'tcd' function;
                        wire it up with: eval "$(tower shell bash)"

run with no args to open the TUI.`)
}
