package refresh

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/itsHabib/tower/internal/domain"
	"github.com/itsHabib/tower/internal/observe"
	"github.com/itsHabib/tower/internal/store"
)

type fakeGit struct {
	worktrees   []observe.Worktree
	dirty       map[string]bool
	ahead       map[string]int
	behind      map[string]int
	lastCommit  map[string]time.Time
	title       map[string]string
	worktreeErr error
}

func (f *fakeGit) Worktrees(_ context.Context) ([]observe.Worktree, error) {
	return f.worktrees, f.worktreeErr
}
func (f *fakeGit) AddWorktree(_ context.Context, _, _ string) error { return nil }
func (f *fakeGit) RemoveWorktree(_ context.Context, _ string) error { return nil }
func (f *fakeGit) Dirty(_ context.Context, p string) (bool, error)  { return f.dirty[p], nil }
func (f *fakeGit) AheadBehind(_ context.Context, p string) (int, int, error) {
	return f.ahead[p], f.behind[p], nil
}
func (f *fakeGit) LastCommit(_ context.Context, p string) (time.Time, string, error) {
	return f.lastCommit[p], f.title[p], nil
}
func (f *fakeGit) MainRoot(_ context.Context) (string, error) {
	if len(f.worktrees) == 0 {
		return "", errors.New("no worktrees")
	}
	return f.worktrees[0].Path, nil
}

type fakeGH struct {
	prByBranch map[string]*domain.PullRequest
	reviews    map[int][]domain.Review
	checks     map[int][]domain.CICheck
	checkErr   error
}

func (f *fakeGH) PullRequestForBranch(_ context.Context, branch string) (*domain.PullRequest, error) {
	pr, ok := f.prByBranch[branch]
	if !ok {
		return nil, nil
	}
	cp := *pr
	return &cp, nil
}
func (f *fakeGH) Reviews(_ context.Context, prNumber int) ([]domain.Review, error) {
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

func TestReconcileInsertsNewWorktrees(t *testing.T) {
	s := newStore(t)
	g := &fakeGit{
		worktrees: []observe.Worktree{
			{Path: "/repo", HEAD: "abc", Branch: "main"},
			{Path: "/repo/.worktrees/feat-x", HEAD: "def", Branch: "tower/feat-x"},
		},
		dirty:      map[string]bool{"/repo/.worktrees/feat-x": true},
		ahead:      map[string]int{"/repo/.worktrees/feat-x": 3},
		behind:     map[string]int{"/repo/.worktrees/feat-x": 1},
		lastCommit: map[string]time.Time{"/repo/.worktrees/feat-x": time.Unix(1700000000, 0).UTC()},
		title:      map[string]string{"/repo/.worktrees/feat-x": "wip"},
	}
	svc := New(s, g, &fakeGH{})

	if err := svc.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	wts, err := s.ListWorktrees(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(wts) != 2 {
		t.Fatalf("want 2 worktrees, got %d: %+v", len(wts), wts)
	}

	feat, _ := s.GetWorktree(context.Background(), "tower/feat-x")
	if feat == nil {
		t.Fatal("missing feat-x")
	}
	if !feat.Dirty || feat.Ahead != 3 || feat.Behind != 1 || feat.Title != "wip" {
		t.Fatalf("enrichment lost: %+v", feat)
	}
}

func TestReconcileSkipsDetached(t *testing.T) {
	s := newStore(t)
	g := &fakeGit{
		worktrees: []observe.Worktree{
			{Path: "/repo", HEAD: "abc", Branch: "main"},
			{Path: "/repo/.worktrees/spike", HEAD: "def"}, // detached
		},
	}
	svc := New(s, g, &fakeGH{})
	_ = svc.Reconcile(context.Background())
	wts, _ := s.ListWorktrees(context.Background())
	if len(wts) != 1 || wts[0].Branch != "main" {
		t.Fatalf("detached should be skipped: %+v", wts)
	}
}

func TestReconcileDeletesStaleWorktrees(t *testing.T) {
	s := newStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	_ = s.UpsertWorktree(context.Background(), domain.Worktree{
		Branch: "tower/old", Path: "/old", CreatedAt: now, LastSeen: now,
	})

	g := &fakeGit{
		worktrees: []observe.Worktree{{Path: "/repo", HEAD: "abc", Branch: "main"}},
	}
	svc := New(s, g, &fakeGH{})
	if err := svc.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	got, _ := s.GetWorktree(context.Background(), "tower/old")
	if got != nil {
		t.Errorf("stale worktree should be deleted, got %+v", got)
	}
}

func TestReconcilePreservesCreatedAt(t *testing.T) {
	s := newStore(t)
	original := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	_ = s.UpsertWorktree(context.Background(), domain.Worktree{
		Branch: "main", Path: "/repo", CreatedAt: original, LastSeen: original,
	})
	g := &fakeGit{
		worktrees: []observe.Worktree{{Path: "/repo", HEAD: "abc", Branch: "main"}},
	}
	svc := New(s, g, &fakeGH{})
	_ = svc.Reconcile(context.Background())
	got, _ := s.GetWorktree(context.Background(), "main")
	if !got.CreatedAt.Equal(original) {
		t.Errorf("created_at should be preserved: want %v got %v", original, got.CreatedAt)
	}
}

func TestBranchPullsPRReviewsChecks(t *testing.T) {
	s := newStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	_ = s.UpsertWorktree(context.Background(), domain.Worktree{
		Branch: "tower/x", Path: "/p", CreatedAt: now, LastSeen: now,
	})

	gh := &fakeGH{
		prByBranch: map[string]*domain.PullRequest{
			"tower/x": {Number: 99, URL: "u", State: domain.PRStateOpen, CreatedAt: now, UpdatedAt: now},
		},
		reviews: map[int][]domain.Review{
			99: {{PRNumber: 99, Reviewer: "claude", State: domain.ReviewApproved, CreatedAt: now}},
		},
		checks: map[int][]domain.CICheck{
			99: {{PRNumber: 99, Name: "build", Conclusion: domain.CISuccess, UpdatedAt: now}},
		},
	}
	svc := New(s, &fakeGit{}, gh)
	if err := svc.Branch(context.Background(), "tower/x"); err != nil {
		t.Fatalf("branch: %v", err)
	}
	pr, _ := s.GetPullRequest(context.Background(), "tower/x")
	if pr == nil || pr.Number != 99 || pr.Branch != "tower/x" {
		t.Fatalf("pr mismatch: %+v", pr)
	}
}

func TestAllReconcilesAndAggregates(t *testing.T) {
	s := newStore(t)
	g := &fakeGit{
		worktrees: []observe.Worktree{
			{Path: "/repo", Branch: "main", HEAD: "a"},
			{Path: "/repo/.worktrees/good", Branch: "tower/good", HEAD: "b"},
			{Path: "/repo/.worktrees/bad", Branch: "tower/bad", HEAD: "c"},
		},
	}
	now := time.Now().UTC().Truncate(time.Second)
	gh := &fakeGH{
		prByBranch: map[string]*domain.PullRequest{
			"tower/good": {Number: 1, State: domain.PRStateOpen, CreatedAt: now, UpdatedAt: now},
			"tower/bad":  {Number: 2, State: domain.PRStateOpen, CreatedAt: now, UpdatedAt: now},
		},
		reviews:  map[int][]domain.Review{1: {}, 2: {}},
		checks:   map[int][]domain.CICheck{1: {}},
		checkErr: errors.New("checks broken"),
	}
	svc := New(s, g, gh)
	res, err := svc.All(context.Background())
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	// main has no PR, so it succeeds as a no-op.
	// tower/good and tower/bad both fail because checkErr is set.
	if res.Synced != 1 {
		t.Errorf("synced: want 1 (main, no PR) got %d", res.Synced)
	}
	if len(res.Errors) != 2 {
		t.Errorf("errors: want 2 (good + bad), got %d", len(res.Errors))
	}
}
