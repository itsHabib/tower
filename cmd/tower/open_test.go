package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/itsHabib/tower/internal/domain"
)

func TestRunOpenPrintsPath(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	if _, err := env.c.workflow.AddRepo(ctx, t.TempDir(), "myrepo"); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	wantPath := "/p/x"
	if err := env.c.store.UpsertWorktree(ctx, domain.Worktree{
		Repo: "myrepo", Branch: "tower/feat-x", Path: wantPath,
		CreatedAt: now, LastSeen: now,
	}); err != nil {
		t.Fatalf("seed worktree: %v", err)
	}

	var buf bytes.Buffer
	if err := runOpen(ctx, env.c, "feat-x", "myrepo", false, &buf); err != nil {
		t.Fatalf("runOpen: %v", err)
	}
	if !strings.Contains(buf.String(), wantPath) {
		t.Errorf("output should contain worktree path %q: %q", wantPath, buf.String())
	}
}
