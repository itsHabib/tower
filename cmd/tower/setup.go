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
	store    store.Store
	workflow *workflow.Service
}

// setup wires the global store, observers, and workflow service. State
// lives at <user-config-dir>/tower/state.db and is shared across every
// repo tower tracks.
func setup(ctx context.Context) (*cliCtx, func(), error) {
	dbPath, err := stateDBPath()
	if err != nil {
		return nil, nil, err
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, nil, fmt.Errorf("create state dir: %w", err)
	}
	s, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		return nil, nil, fmt.Errorf("open store: %w", err)
	}
	gitFactory := func(repoPath string) observe.Git { return observe.NewGit(repoPath) }
	ghFactory := func(repoPath string) observe.GH { return observe.NewGH(repoPath) }
	ref := refresh.New(s, gitFactory, ghFactory)
	wf := workflow.New(workflow.Config{}, s, gitFactory, ref)
	cleanup := func() { _ = s.Close() }
	return &cliCtx{store: s, workflow: wf}, cleanup, nil
}

func stateDBPath() (string, error) {
	cfg, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("user config dir: %w", err)
	}
	return filepath.Join(cfg, "tower", "state.db"), nil
}

// gitTopLevel runs `git rev-parse --show-toplevel` from cwd.
func gitTopLevel(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse --show-toplevel: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
