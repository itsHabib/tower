// seed builds a richer playground for manual TUI poking: ~6 fake repos
// with a handful of worktrees each, in varied state. Drives the same
// workflow.Service the real binary uses so the resulting state.db is
// indistinguishable from one built up by hand.
//
// Run via the wrapper scripts (`setup-test-env.sh` / `.ps1`) so the
// state.db lands under the sandbox APPDATA — running this directly
// will write to your real tower state if APPDATA isn't overridden.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/itsHabib/tower/internal/observe"
	"github.com/itsHabib/tower/internal/playground"
	"github.com/itsHabib/tower/internal/refresh"
	"github.com/itsHabib/tower/internal/store"
	"github.com/itsHabib/tower/internal/workflow"
)

func main() {
	var rootFlag, stateFlag string
	flag.StringVar(&rootFlag, "root", "", "directory to create fake repos under (required)")
	flag.StringVar(&stateFlag, "state", "", "directory to use as APPDATA for tower state (required)")
	flag.Parse()

	if rootFlag == "" || stateFlag == "" {
		fmt.Fprintln(os.Stderr, "usage: seed -root <repos-dir> -state <state-dir>")
		os.Exit(2)
	}

	ctx := context.Background()

	dbPath := filepath.Join(stateFlag, "tower", "state.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		die(err)
	}

	s, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		die(fmt.Errorf("open store: %w", err))
	}
	defer func() { _ = s.Close() }()

	gitFactory := func(p string) observe.Git { return observe.NewGit(p) }
	ghFactory := func(p string) observe.GH { return observe.NewGH(p) }
	ref := refresh.New(s, gitFactory, ghFactory)
	wf := workflow.New(workflow.Config{}, s, gitFactory, ref)

	progress := func(msg string) { fmt.Println(msg) }
	res, err := playground.Seed(ctx, wf, rootFlag, playground.Default, progress)
	if err != nil {
		die(err)
	}
	fmt.Printf("seeded %d repos / %d worktrees under %s\n", res.Repos, res.Worktrees, rootFlag)
}

func die(err error) {
	fmt.Fprintln(os.Stderr, "seed: error:", err)
	os.Exit(1)
}
