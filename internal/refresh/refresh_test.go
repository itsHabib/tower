package refresh

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/itsHabib/tower/internal/domain"
	"github.com/itsHabib/tower/internal/store"
)

type fakeGH struct {
	prByBranch map[string]*domain.PullRequest
	reviews    map[int][]domain.Review
	checks     map[int][]domain.CICheck
	prErr      error
	reviewErr  error
	checkErr   error
}

func (f *fakeGH) PullRequestForBranch(_ context.Context, branch string) (*domain.PullRequest, error) {
	if f.prErr != nil {
		return nil, f.prErr
	}
	pr, ok := f.prByBranch[branch]
	if !ok {
		return nil, nil
	}
	cp := *pr
	return &cp, nil
}

func (f *fakeGH) Reviews(_ context.Context, prNumber int) ([]domain.Review, error) {
	if f.reviewErr != nil {
		return nil, f.reviewErr
	}
	return f.reviews[prNumber], nil
}

func (f *fakeGH) Checks(_ context.Context, prNumber int) ([]domain.CICheck, error) {
	if f.checkErr != nil {
		return nil, f.checkErr
	}
	return f.checks[prNumber], nil
}

func newStore(t *testing.T) store.Store {
	t.Helper()
	s, err := store.OpenSQLite(context.Background(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func seedTaskWithWorktree(t *testing.T, s store.Store, id, branch string) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	if err := s.UpsertTask(context.Background(), domain.Task{
		ID: id, Title: id, Path: "/p", Status: domain.StatusActive,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("upsert task: %v", err)
	}
	if err := s.SetWorktree(context.Background(), domain.Worktree{
		TaskID: id, Path: "/repo/.worktrees/" + id, Branch: branch, CreatedAt: now,
	}); err != nil {
		t.Fatalf("set worktree: %v", err)
	}
}

func TestTaskNoWorktree(t *testing.T) {
	s := newStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	if err := s.UpsertTask(context.Background(), domain.Task{
		ID: "t1", Title: "T1", Path: "/p", Status: domain.StatusDraft,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	svc := New(s, &fakeGH{})
	if err := svc.Task(context.Background(), "t1"); err != nil {
		t.Fatalf("task: %v", err)
	}
	pr, _ := s.GetPullRequest(context.Background(), "t1")
	if pr != nil {
		t.Fatalf("expected no pr, got %+v", pr)
	}
}

func TestTaskNoPR(t *testing.T) {
	s := newStore(t)
	seedTaskWithWorktree(t, s, "t1", "tower/t1")
	svc := New(s, &fakeGH{prByBranch: map[string]*domain.PullRequest{}})
	if err := svc.Task(context.Background(), "t1"); err != nil {
		t.Fatalf("task: %v", err)
	}
	pr, _ := s.GetPullRequest(context.Background(), "t1")
	if pr != nil {
		t.Fatalf("expected no pr, got %+v", pr)
	}
}

func TestTaskFullPath(t *testing.T) {
	s := newStore(t)
	seedTaskWithWorktree(t, s, "t1", "tower/t1")

	now := time.Now().UTC().Truncate(time.Second)
	gh := &fakeGH{
		prByBranch: map[string]*domain.PullRequest{
			"tower/t1": {Number: 99, URL: "https://gh/pr/99", State: domain.PRStateOpen, Title: "T1", CreatedAt: now, UpdatedAt: now},
		},
		reviews: map[int][]domain.Review{
			99: {
				{PRNumber: 99, Reviewer: "claude", State: domain.ReviewCommented, Body: "nit", CreatedAt: now},
				{PRNumber: 99, Reviewer: "copilot", State: domain.ReviewApproved, Body: "lgtm", CreatedAt: now.Add(time.Minute)},
			},
		},
		checks: map[int][]domain.CICheck{
			99: {
				{PRNumber: 99, Name: "build", Conclusion: domain.CISuccess, UpdatedAt: now},
				{PRNumber: 99, Name: "test", Conclusion: domain.CIFailure, URL: "https://ci/log", UpdatedAt: now},
			},
		},
	}
	svc := New(s, gh)
	if err := svc.Task(context.Background(), "t1"); err != nil {
		t.Fatalf("task: %v", err)
	}

	pr, err := s.GetPullRequest(context.Background(), "t1")
	if err != nil {
		t.Fatalf("get pr: %v", err)
	}
	if pr == nil || pr.Number != 99 || pr.TaskID != "t1" {
		t.Fatalf("pr mismatch: %+v", pr)
	}
	reviews, _ := s.ListReviews(context.Background(), 99)
	if len(reviews) != 2 {
		t.Fatalf("want 2 reviews, got %d", len(reviews))
	}
	checks, _ := s.ListCIChecks(context.Background(), 99)
	if len(checks) != 2 {
		t.Fatalf("want 2 checks, got %d", len(checks))
	}
}

func TestAllAggregatesErrors(t *testing.T) {
	s := newStore(t)
	seedTaskWithWorktree(t, s, "good", "tower/good")
	seedTaskWithWorktree(t, s, "bad", "tower/bad")

	now := time.Now().UTC().Truncate(time.Second)
	gh := &fakeGH{
		prByBranch: map[string]*domain.PullRequest{
			"tower/good": {Number: 1, URL: "u", State: domain.PRStateOpen, CreatedAt: now, UpdatedAt: now},
			"tower/bad":  {Number: 2, URL: "u", State: domain.PRStateOpen, CreatedAt: now, UpdatedAt: now},
		},
		reviews:   map[int][]domain.Review{1: {}, 2: {}},
		checks:    map[int][]domain.CICheck{1: {}},
		checkErr:  nil,
		reviewErr: nil,
	}
	gh.checkErr = errors.New("simulated checks failure")

	svc := New(s, gh)
	res, err := svc.All(context.Background())
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if res.Synced != 0 {
		t.Fatalf("checks always fail in this test, expected 0 synced, got %d", res.Synced)
	}
	if len(res.Errors) != 2 {
		t.Fatalf("expected 2 errors, got %d", len(res.Errors))
	}
}
