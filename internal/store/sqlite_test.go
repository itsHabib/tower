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

func TestWorktreeRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	want := domain.Worktree{
		Branch:     "tower/feat-x",
		Path:       "/repo/.worktrees/feat-x",
		HEAD:       "abc123",
		Title:      "wip: refactor",
		Dirty:      true,
		Ahead:      3,
		Behind:     1,
		LastCommit: now.Add(-time.Hour),
		CreatedAt:  now.Add(-2 * time.Hour),
		LastSeen:   now,
	}
	if err := s.UpsertWorktree(ctx, want); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := s.GetWorktree(ctx, want.Branch)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("got nil")
	}
	if got.Branch != want.Branch || got.Path != want.Path || got.HEAD != want.HEAD ||
		got.Title != want.Title || got.Dirty != want.Dirty ||
		got.Ahead != want.Ahead || got.Behind != want.Behind {
		t.Fatalf("mismatch:\nwant %+v\ngot  %+v", want, *got)
	}
	if !got.LastCommit.Equal(want.LastCommit) {
		t.Errorf("last_commit: want %v got %v", want.LastCommit, got.LastCommit)
	}

	want.Dirty = false
	want.Ahead = 5
	want.LastSeen = now.Add(time.Minute)
	if err := s.UpsertWorktree(ctx, want); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	got, _ = s.GetWorktree(ctx, want.Branch)
	if got.Dirty || got.Ahead != 5 {
		t.Fatalf("update mismatch: %+v", got)
	}
}

func TestWorktreeWithoutLastCommit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	w := domain.Worktree{
		Branch: "tower/empty", Path: "/p", HEAD: "x",
		CreatedAt: now, LastSeen: now,
	}
	if err := s.UpsertWorktree(ctx, w); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, _ := s.GetWorktree(ctx, "tower/empty")
	if !got.LastCommit.IsZero() {
		t.Errorf("expected zero last_commit, got %v", got.LastCommit)
	}
}

func TestListWorktreesOrder(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	for i, branch := range []string{"a", "b", "c"} {
		_ = s.UpsertWorktree(ctx, domain.Worktree{
			Branch: branch, Path: "/" + branch,
			CreatedAt: now, LastSeen: now.Add(time.Duration(i) * time.Minute),
		})
	}
	all, err := s.ListWorktrees(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 3 || all[0].Branch != "c" || all[2].Branch != "a" {
		t.Fatalf("expected last-seen-desc order: %+v", all)
	}
}

func TestPullRequestRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	if err := s.UpsertWorktree(ctx, domain.Worktree{Branch: "tower/x", Path: "/p", CreatedAt: now, LastSeen: now}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	pr := domain.PullRequest{
		Branch:    "tower/x",
		Number:    42,
		URL:       "https://github.com/x/y/pull/42",
		State:     domain.PRStateOpen,
		Title:     "Feature X",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.SetPullRequest(ctx, pr); err != nil {
		t.Fatalf("set pr: %v", err)
	}
	got, err := s.GetPullRequest(ctx, "tower/x")
	if err != nil {
		t.Fatalf("get pr: %v", err)
	}
	if got.Number != 42 || got.State != domain.PRStateOpen {
		t.Fatalf("pr mismatch: %+v", got)
	}

	if err := s.DeleteWorktree(ctx, "tower/x"); err != nil {
		t.Fatalf("delete worktree: %v", err)
	}
	got, err = s.GetPullRequest(ctx, "tower/x")
	if err != nil {
		t.Fatalf("get pr after cascade: %v", err)
	}
	if got != nil {
		t.Fatalf("pr should cascade away with worktree: %+v", got)
	}
}

func TestReviewsAndChecks(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	reviews := []domain.Review{
		{PRNumber: 7, Reviewer: "claude", State: domain.ReviewCommented, CreatedAt: now},
		{PRNumber: 7, Reviewer: "copilot", State: domain.ReviewApproved, CreatedAt: now.Add(time.Minute)},
	}
	for _, r := range reviews {
		if err := s.UpsertReview(ctx, r); err != nil {
			t.Fatalf("upsert review: %v", err)
		}
	}
	got, _ := s.ListReviews(ctx, 7)
	if len(got) != 2 {
		t.Fatalf("want 2 reviews, got %d", len(got))
	}

	checks := []domain.CICheck{
		{PRNumber: 7, Name: "build", Conclusion: domain.CISuccess, UpdatedAt: now},
		{PRNumber: 7, Name: "test", Conclusion: domain.CIFailure, UpdatedAt: now},
	}
	for _, c := range checks {
		if err := s.UpsertCICheck(ctx, c); err != nil {
			t.Fatalf("upsert check: %v", err)
		}
	}
	gotChecks, _ := s.ListCIChecks(ctx, 7)
	if len(gotChecks) != 2 {
		t.Fatalf("want 2 checks, got %d", len(gotChecks))
	}
}
