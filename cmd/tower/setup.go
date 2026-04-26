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

func setup(ctx context.Context) (*cliCtx, func(), error) {
	repo, err := repoRoot(ctx)
	if err != nil {
		return nil, nil, err
	}
	dbPath := filepath.Join(repo, ".tower", "state.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, nil, fmt.Errorf("create state dir: %w", err)
	}
	s, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		return nil, nil, fmt.Errorf("open store: %w", err)
	}
	git := observe.NewGit(repo)
	gh := observe.NewGH(repo)
	ref := refresh.New(s, gh)
	wf := workflow.New(workflow.Config{Repo: repo}, s, git, ref)
	cleanup := func() { _ = s.Close() }
	return &cliCtx{repo: repo, store: s, workflow: wf}, cleanup, nil
}

func repoRoot(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse --show-toplevel: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
