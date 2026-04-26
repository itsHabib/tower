package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/itsHabib/tower/internal/domain"
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	dir := t.TempDir()
	s, err := OpenSQLite(context.Background(), filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func mustRepo(t *testing.T, s Store, name, path string) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	if err := s.UpsertRepo(context.Background(), domain.Repo{
		Name: name, Path: path, CreatedAt: now,
	}); err != nil {
		t.Fatalf("upsert repo: %v", err)
	}
}

func TestRepoRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	r := domain.Repo{Name: "orchestra", Path: "/pers/orchestra", CreatedAt: now}
	if err := s.UpsertRepo(ctx, r); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err := s.GetRepo(ctx, "orchestra")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil || got.Path != r.Path {
		t.Fatalf("repo mismatch: %+v", got)
	}
	all, _ := s.ListRepos(ctx)
	if len(all) != 1 {
		t.Fatalf("want 1, got %d", len(all))
	}
	if err := s.DeleteRepo(ctx, "orchestra"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, _ = s.GetRepo(ctx, "orchestra")
	if got != nil {
		t.Fatalf("expected nil after delete, got %+v", got)
	}
}

func TestWorktreeScopedByRepo(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	mustRepo(t, s, "orchestra", "/pers/orchestra")
	mustRepo(t, s, "roxiq", "/pers/roxiq")

	a := domain.Worktree{Repo: "orchestra", Branch: "tower/x", Path: "/o/wt", CreatedAt: now, LastSeen: now}
	b := domain.Worktree{Repo: "roxiq", Branch: "tower/x", Path: "/r/wt", CreatedAt: now, LastSeen: now}
	if err := s.UpsertWorktree(ctx, a); err != nil {
		t.Fatalf("upsert a: %v", err)
	}
	if err := s.UpsertWorktree(ctx, b); err != nil {
		t.Fatalf("upsert b: %v", err)
	}

	gotA, _ := s.GetWorktree(ctx, "orchestra", "tower/x")
	gotB, _ := s.GetWorktree(ctx, "roxiq", "tower/x")
	if gotA == nil || gotA.Path != "/o/wt" {
		t.Fatalf("orchestra worktree wrong: %+v", gotA)
	}
	if gotB == nil || gotB.Path != "/r/wt" {
		t.Fatalf("roxiq worktree wrong: %+v", gotB)
	}

	all, _ := s.ListWorktrees(ctx)
	if len(all) != 2 {
		t.Fatalf("list all: want 2, got %d", len(all))
	}
	scoped, _ := s.ListWorktreesForRepo(ctx, "orchestra")
	if len(scoped) != 1 || scoped[0].Path != "/o/wt" {
		t.Fatalf("list orchestra: %+v", scoped)
	}
}

func TestRepoDeleteCascadesWorktreesAndPRs(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	mustRepo(t, s, "orchestra", "/pers/orchestra")
	_ = s.UpsertWorktree(ctx, domain.Worktree{
		Repo: "orchestra", Branch: "tower/x", Path: "/p", CreatedAt: now, LastSeen: now,
	})
	_ = s.SetPullRequest(ctx, domain.PullRequest{
		Repo: "orchestra", Branch: "tower/x", Number: 1, URL: "u",
		State: domain.PRStateOpen, CreatedAt: now, UpdatedAt: now,
	})

	if err := s.DeleteRepo(ctx, "orchestra"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	wt, _ := s.GetWorktree(ctx, "orchestra", "tower/x")
	if wt != nil {
		t.Errorf("worktree should cascade: %+v", wt)
	}
	pr, _ := s.GetPullRequest(ctx, "orchestra", "tower/x")
	if pr != nil {
		t.Errorf("pr should cascade: %+v", pr)
	}
}

func TestReviewsAndChecksScopedByRepo(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	mustRepo(t, s, "a", "/a")
	mustRepo(t, s, "b", "/b")

	rA := domain.Review{Repo: "a", PRNumber: 1, Reviewer: "claude", State: domain.ReviewApproved, CreatedAt: now}
	rB := domain.Review{Repo: "b", PRNumber: 1, Reviewer: "claude", State: domain.ReviewCommented, CreatedAt: now}
	_ = s.UpsertReview(ctx, rA)
	_ = s.UpsertReview(ctx, rB)

	listA, _ := s.ListReviews(ctx, "a", 1)
	if len(listA) != 1 || listA[0].State != domain.ReviewApproved {
		t.Fatalf("a reviews: %+v", listA)
	}
	listB, _ := s.ListReviews(ctx, "b", 1)
	if len(listB) != 1 || listB[0].State != domain.ReviewCommented {
		t.Fatalf("b reviews: %+v", listB)
	}

	cA := domain.CICheck{Repo: "a", PRNumber: 1, Name: "build", Conclusion: domain.CISuccess, UpdatedAt: now}
	cB := domain.CICheck{Repo: "b", PRNumber: 1, Name: "build", Conclusion: domain.CIFailure, UpdatedAt: now}
	_ = s.UpsertCICheck(ctx, cA)
	_ = s.UpsertCICheck(ctx, cB)

	checksA, _ := s.ListCIChecks(ctx, "a", 1)
	checksB, _ := s.ListCIChecks(ctx, "b", 1)
	if len(checksA) != 1 || checksA[0].Conclusion != domain.CISuccess {
		t.Fatalf("a checks: %+v", checksA)
	}
	if len(checksB) != 1 || checksB[0].Conclusion != domain.CIFailure {
		t.Fatalf("b checks: %+v", checksB)
	}
}
