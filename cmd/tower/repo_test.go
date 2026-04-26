package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunRepoAddRegistersByName(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	repoPath := t.TempDir()

	var buf bytes.Buffer
	if err := runRepoAdd(ctx, env.c, repoPath, "myrepo", &buf); err != nil {
		t.Fatalf("runRepoAdd: %v", err)
	}
	got, err := env.c.store.GetRepo(ctx, "myrepo")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("repo not registered")
	}
	if got.Path != repoPath {
		t.Errorf("path: want %q got %q", repoPath, got.Path)
	}
	if !strings.Contains(buf.String(), "registered: myrepo") {
		t.Errorf("output: %q", buf.String())
	}
}

func TestRunRepoLsListsBoth(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	if _, err := env.c.workflow.AddRepo(ctx, t.TempDir(), "alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.c.workflow.AddRepo(ctx, t.TempDir(), "beta"); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := runRepoLs(ctx, env.c, &buf); err != nil {
		t.Fatalf("runRepoLs: %v", err)
	}
	out := buf.String()
	for _, name := range []string{"alpha", "beta"} {
		if !strings.Contains(out, name) {
			t.Errorf("output missing %q: %s", name, out)
		}
	}
}

func TestRunRepoRmDeletes(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	if _, err := env.c.workflow.AddRepo(ctx, t.TempDir(), "myrepo"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var buf bytes.Buffer
	if err := runRepoRm(ctx, env.c, "myrepo", &buf); err != nil {
		t.Fatalf("runRepoRm: %v", err)
	}
	got, _ := env.c.store.GetRepo(ctx, "myrepo")
	if got != nil {
		t.Errorf("repo should be deleted: %+v", got)
	}
	if !strings.Contains(buf.String(), "unregistered: myrepo") {
		t.Errorf("output: %q", buf.String())
	}
}

func TestRunRepoPruneRemovesMissingPaths(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	// Register a repo at a path that does not exist on disk.
	if _, err := env.c.workflow.AddRepo(ctx, "/definitely/not/a/real/path/tower-test", "ghost"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var buf bytes.Buffer
	if err := runRepoPrune(ctx, env.c, false, &buf); err != nil {
		t.Fatalf("runRepoPrune: %v", err)
	}
	got, _ := env.c.store.GetRepo(ctx, "ghost")
	if got != nil {
		t.Errorf("ghost repo should have been pruned: %+v", got)
	}
	if !strings.Contains(buf.String(), "pruned: ghost") {
		t.Errorf("output: %q", buf.String())
	}
}
