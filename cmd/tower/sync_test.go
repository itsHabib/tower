package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/itsHabib/tower/internal/observe"
)

func TestRunSyncReportsCount(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	repoPath := t.TempDir()
	if _, err := env.c.workflow.AddRepo(ctx, repoPath, "myrepo"); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	// fakeGit returns one matching worktree so reconcile keeps it.
	env.git.worktrees = []observe.Worktree{
		{Path: repoPath + "/.worktrees/feat-x", HEAD: "abc", Branch: "tower/feat-x"},
	}

	var out, errOut bytes.Buffer
	if err := runSync(ctx, env.c, &out, &errOut); err != nil {
		t.Fatalf("runSync: %v", err)
	}
	if !strings.Contains(out.String(), "sync: 1 ok, 0 errors") {
		t.Errorf("expected '1 ok, 0 errors' in output: %q", out.String())
	}
}

func TestRunReconcileUpsertsLiveWorktree(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	repoPath := t.TempDir()
	if _, err := env.c.workflow.AddRepo(ctx, repoPath, "myrepo"); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	env.git.worktrees = []observe.Worktree{
		{Path: repoPath + "/.worktrees/feat-x", HEAD: "abc", Branch: "tower/feat-x"},
	}

	var buf bytes.Buffer
	if err := runReconcile(ctx, env.c, &buf); err != nil {
		t.Fatalf("runReconcile: %v", err)
	}
	if !strings.Contains(buf.String(), "reconcile: ok") {
		t.Errorf("output: %q", buf.String())
	}
	got, err := env.c.store.GetWorktree(ctx, "myrepo", "tower/feat-x")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("worktree not upserted by reconcile")
	}
}
