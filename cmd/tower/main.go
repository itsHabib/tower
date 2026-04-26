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
	case "discover":
		return cmdDiscover(rest)
	case "add":
		return cmdAdd(rest)
	case "rm":
		return cmdRm(rest)
	case "sync":
		return cmdSync(rest)
	case "ls":
		return cmdLs(rest)
	case "open":
		return cmdOpen(rest)
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
	fmt.Fprintln(os.Stderr, `tower — control tower for parallel agentic PR work

usage: tower <command> [args...]

commands:
  discover [-d <dir>]   scan task markdown files (default: features/)
  add <id>              create worktree at .worktrees/<id> on branch tower/<id>
  rm <id>               remove worktree, mark task abandoned
  sync                  refresh PR / review / CI state from GitHub
  ls                    list tasks with status
  open <id>             print worktree path (use: cd $(tower open <id>))
  help                  this message

run with no args to open the TUI.`)
}
