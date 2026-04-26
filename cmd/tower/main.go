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
	fmt.Fprintln(os.Stderr, `tower — manage parallel git worktrees

usage: tower <command> [args...]

commands:
  add <name>       create a worktree at .worktrees/<name> on branch tower/<name>
                   (a name with a slash is used as the full branch ref)
  rm <name>        tear down the worktree for the named branch
  ls               list tracked worktrees with status
  open <name>      print worktree path (use: cd $(tower open <name>))
  sync             reconcile from git + refresh PR / review / CI from GitHub
  reconcile        reconcile from git only (no network)
  help             this message

run with no args to open the TUI.`)
}
