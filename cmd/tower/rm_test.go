package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunRmTearsDownWorktree(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	repoPath := t.TempDir()
	if _, err := env.c.workflow.AddRepo(ctx, repoPath, "myrepo"); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	wt, err := env.c.workflow.Add(ctx, "myrepo", "feat-x")
	if err != nil {
		t.Fatalf("seed worktree: %v", err)
	}

	var buf bytes.Buffer
	if err := runRm(ctx, env.c, "feat-x", "myrepo", &buf); err != nil {
		t.Fatalf("runRm: %v", err)
	}

	got, err := env.c.store.GetWorktree(ctx, "myrepo", "tower/feat-x")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("worktree should be deleted from store: %+v", got)
	}
	if len(env.git.removedPaths) != 1 || env.git.removedPaths[0] != wt.Path {
		t.Errorf("git.RemoveWorktree calls: %v (want [%s])", env.git.removedPaths, wt.Path)
	}
	if !strings.Contains(buf.String(), "removed: myrepo/feat-x") {
		t.Errorf("output missing confirmation: %q", buf.String())
	}
}
