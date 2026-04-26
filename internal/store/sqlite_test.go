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

func TestTaskRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	want := domain.Task{
		ID:        "feat-login",
		Title:     "Add login flow",
		Brief:     "## summary\nAuth via OAuth.",
		Path:      "/repo/features/feat-login.md",
		Deps:      []string{"feat-users", "feat-sessions"},
		Status:    domain.StatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.UpsertTask(ctx, want); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := s.GetTask(ctx, want.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("got nil task")
	}
	assertTaskEqual(t, want, *got)

	// Update via upsert.
	want.Title = "Add SSO login flow"
	want.Status = domain.StatusBlocked
	want.UpdatedAt = now.Add(time.Minute)
	if err := s.UpsertTask(ctx, want); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	got, err = s.GetTask(ctx, want.ID)
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	assertTaskEqual(t, want, *got)

	all, err := s.ListTasks(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("want 1 task, got %d", len(all))
	}

	if err := s.DeleteTask(ctx, want.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, err = s.GetTask(ctx, want.ID)
	if err != nil {
		t.Fatalf("get after delete: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil after delete, got %+v", got)
	}
}

func TestGetTaskMissing(t *testing.T) {
	s := newTestStore(t)
	got, err := s.GetTask(context.Background(), "nope")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestWorktreeRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	if err := s.UpsertTask(ctx, domain.Task{ID: "t1", Title: "T1", Path: "/p", Status: domain.StatusActive, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("upsert task: %v", err)
	}

	wt := domain.Worktree{TaskID: "t1", Path: "/repo/.worktrees/t1", Branch: "tower/t1", CreatedAt: now}
	if err := s.SetWorktree(ctx, wt); err != nil {
		t.Fatalf("set worktree: %v", err)
	}

	got, err := s.GetWorktree(ctx, "t1")
	if err != nil {
		t.Fatalf("get worktree: %v", err)
	}
	if got == nil || got.Path != wt.Path || got.Branch != wt.Branch {
		t.Fatalf("worktree mismatch: %+v vs %+v", got, wt)
	}

	wt.Branch = "tower/t1-v2"
	if err := s.SetWorktree(ctx, wt); err != nil {
		t.Fatalf("re-set worktree: %v", err)
	}
	got, _ = s.GetWorktree(ctx, "t1")
	if got.Branch != "tower/t1-v2" {
		t.Fatalf("want updated branch, got %q", got.Branch)
	}

	if err := s.DeleteTask(ctx, "t1"); err != nil {
		t.Fatalf("delete task: %v", err)
	}
	got, err = s.GetWorktree(ctx, "t1")
	if err != nil {
		t.Fatalf("get worktree after task delete: %v", err)
	}
	if got != nil {
		t.Fatalf("expected worktree cascaded away, got %+v", got)
	}
}

func TestPullRequestRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	if err := s.UpsertTask(ctx, domain.Task{ID: "t1", Title: "T1", Path: "/p", Status: domain.StatusActive, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("upsert task: %v", err)
	}

	pr := domain.PullRequest{
		TaskID:    "t1",
		Number:    42,
		URL:       "https://github.com/x/y/pull/42",
		State:     domain.PRStateOpen,
		Title:     "T1",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.SetPullRequest(ctx, pr); err != nil {
		t.Fatalf("set pr: %v", err)
	}

	got, err := s.GetPullRequest(ctx, "t1")
	if err != nil {
		t.Fatalf("get pr: %v", err)
	}
	if got.Number != 42 || got.State != domain.PRStateOpen {
		t.Fatalf("pr mismatch: %+v", got)
	}

	pr.State = domain.PRStateMerged
	pr.UpdatedAt = now.Add(time.Hour)
	if err := s.SetPullRequest(ctx, pr); err != nil {
		t.Fatalf("re-set pr: %v", err)
	}
	got, _ = s.GetPullRequest(ctx, "t1")
	if got.State != domain.PRStateMerged {
		t.Fatalf("want merged, got %q", got.State)
	}
}

func TestReviewsAndCIChecks(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	reviews := []domain.Review{
		{PRNumber: 7, Reviewer: "claude", State: domain.ReviewCommented, Body: "nit: rename foo", CreatedAt: now},
		{PRNumber: 7, Reviewer: "copilot", State: domain.ReviewApproved, Body: "lgtm", CreatedAt: now.Add(time.Minute)},
		{PRNumber: 7, Reviewer: "codex", State: domain.ReviewChangesRequested, Body: "missing tests", CreatedAt: now.Add(2 * time.Minute)},
	}
	for _, r := range reviews {
		if err := s.UpsertReview(ctx, r); err != nil {
			t.Fatalf("upsert review: %v", err)
		}
	}
	got, err := s.ListReviews(ctx, 7)
	if err != nil {
		t.Fatalf("list reviews: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 reviews, got %d", len(got))
	}
	if got[0].Reviewer != "claude" || got[2].Reviewer != "codex" {
		t.Fatalf("reviews not ordered by created_at: %+v", got)
	}

	checks := []domain.CICheck{
		{PRNumber: 7, Name: "build", Conclusion: domain.CISuccess, UpdatedAt: now},
		{PRNumber: 7, Name: "test", Conclusion: domain.CIFailure, URL: "https://ci/log", UpdatedAt: now},
	}
	for _, c := range checks {
		if err := s.UpsertCICheck(ctx, c); err != nil {
			t.Fatalf("upsert ci: %v", err)
		}
	}
	checks[1].Conclusion = domain.CISuccess
	if err := s.UpsertCICheck(ctx, checks[1]); err != nil {
		t.Fatalf("re-upsert ci: %v", err)
	}
	gotChecks, err := s.ListCIChecks(ctx, 7)
	if err != nil {
		t.Fatalf("list ci: %v", err)
	}
	if len(gotChecks) != 2 {
		t.Fatalf("want 2 checks, got %d", len(gotChecks))
	}
	for _, c := range gotChecks {
		if c.Conclusion != domain.CISuccess {
			t.Fatalf("expected all success, got %+v", c)
		}
	}
}

func assertTaskEqual(t *testing.T, want, got domain.Task) {
	t.Helper()
	if want.ID != got.ID || want.Title != got.Title || want.Brief != got.Brief ||
		want.Path != got.Path || want.Status != got.Status {
		t.Errorf("task mismatch:\nwant: %+v\ngot:  %+v", want, got)
	}
	if len(want.Deps) != len(got.Deps) {
		t.Errorf("deps length: want %d got %d", len(want.Deps), len(got.Deps))
		return
	}
	for i := range want.Deps {
		if want.Deps[i] != got.Deps[i] {
			t.Errorf("dep[%d]: want %q got %q", i, want.Deps[i], got.Deps[i])
		}
	}
	if !want.CreatedAt.Equal(got.CreatedAt) {
		t.Errorf("created_at: want %v got %v", want.CreatedAt, got.CreatedAt)
	}
	if !want.UpdatedAt.Equal(got.UpdatedAt) {
		t.Errorf("updated_at: want %v got %v", want.UpdatedAt, got.UpdatedAt)
	}
}
