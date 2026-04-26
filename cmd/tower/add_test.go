package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunAddCreatesWorktree(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	repoPath := t.TempDir()
	if _, err := env.c.workflow.AddRepo(ctx, repoPath, "myrepo"); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	var buf bytes.Buffer
	if err := runAdd(ctx, env.c, "feat-x", "myrepo", &buf); err != nil {
		t.Fatalf("runAdd: %v", err)
	}

	wt, err := env.c.store.GetWorktree(ctx, "myrepo", "tower/feat-x")
	if err != nil {
		t.Fatal(err)
	}
	if wt == nil {
		t.Fatal("worktree not persisted")
	}
	wantPath := filepath.Join(repoPath, ".worktrees", "feat-x")
	if wt.Path != wantPath {
		t.Errorf("path: want %q got %q", wantPath, wt.Path)
	}
	if env.git.addedBranch != "tower/feat-x" {
		t.Errorf("git.AddWorktree branch: want tower/feat-x got %q", env.git.addedBranch)
	}
	if !strings.Contains(buf.String(), "added: myrepo/tower/feat-x") {
		t.Errorf("output missing confirmation line: %q", buf.String())
	}
}
