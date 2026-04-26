package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/itsHabib/tower/internal/observe"
	"github.com/itsHabib/tower/internal/refresh"
	"github.com/itsHabib/tower/internal/store"
	"github.com/itsHabib/tower/internal/workflow"
)

type cliCtx struct {
	repo     string
	store    store.Store
	workflow *workflow.Service
}

// setup wires the store, observers, and workflow service. The state DB
// always lives in the main worktree's .tower/, regardless of which
// worktree the user invoked tower from.
func setup(ctx context.Context) (*cliCtx, func(), error) {
	cwdRepo, err := repoTopLevel(ctx)
	if err != nil {
		return nil, nil, err
	}
	git := observe.NewGit(cwdRepo)
	mainRoot, err := git.MainRoot(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("locate main worktree: %w", err)
	}
	dbPath := filepath.Join(mainRoot, ".tower", "state.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, nil, fmt.Errorf("create state dir: %w", err)
	}
	s, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		return nil, nil, fmt.Errorf("open store: %w", err)
	}
	mainGit := observe.NewGit(mainRoot)
	gh := observe.NewGH(mainRoot)
	ref := refresh.New(s, mainGit, gh)
	wf := workflow.New(workflow.Config{Repo: mainRoot}, s, mainGit, ref)
	cleanup := func() { _ = s.Close() }
	return &cliCtx{repo: mainRoot, store: s, workflow: wf}, cleanup, nil
}

func repoTopLevel(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse --show-toplevel: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
