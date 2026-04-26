package workflow

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/itsHabib/tower/internal/domain"
	"github.com/itsHabib/tower/internal/observe"
	"github.com/itsHabib/tower/internal/refresh"
	"github.com/itsHabib/tower/internal/store"
)

type fakeGit struct {
	added   struct{ path, branch string }
	removed string
	addErr  error
}

func (f *fakeGit) Worktrees(_ context.Context) ([]observe.Worktree, error) { return nil, nil }
func (f *fakeGit) AddWorktree(_ context.Context, path, branch string) error {
	if f.addErr != nil {
		return f.addErr
	}
	f.added.path, f.added.branch = path, branch
	return nil
}
func (f *fakeGit) RemoveWorktree(_ context.Context, path string) error {
	f.removed = path
	return nil
}
func (f *fakeGit) Dirty(_ context.Context, _ string) (bool, error)           { return false, nil }
func (f *fakeGit) AheadBehind(_ context.Context, _ string) (int, int, error) { return 0, 0, nil }
func (f *fakeGit) LastCommit(_ context.Context, _ string) (time.Time, string, error) {
	return time.Time{}, "", nil
}
func (f *fakeGit) MainRoot(_ context.Context) (string, error) { return "/repo", nil }

type fakeGH struct{}

func (fakeGH) PullRequestForBranch(context.Context, string) (*domain.PullRequest, error) {
	return nil, nil
}
func (fakeGH) Reviews(context.Context, int) ([]domain.Review, error) { return nil, nil }
func (fakeGH) Checks(context.Context, int) ([]domain.CICheck, error) { return nil, nil }

func newSvc(t *testing.T, git observe.Git) (*Service, store.Store) {
	t.Helper()
	s, err := store.OpenSQLite(context.Background(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ref := refresh.New(s, git, fakeGH{})
	svc := New(Config{Repo: "/repo"}, s, git, ref)
	svc.now = func() time.Time { return time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC) }
	return svc, s
}

func TestAddCreatesWorktreeAtConventionalPath(t *testing.T) {
	g := &fakeGit{}
	svc, s := newSvc(t, g)
	w, err := svc.Add(context.Background(), "feat-x")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	wantPath := filepath.Join("/repo", ".worktrees", "feat-x")
	if w.Path != wantPath {
		t.Errorf("path: want %q got %q", wantPath, w.Path)
	}
	if w.Branch != "tower/feat-x" {
		t.Errorf("branch: %q", w.Branch)
	}
	if g.added.path != wantPath || g.added.branch != "tower/feat-x" {
		t.Errorf("git not called as expected: %+v", g.added)
	}
	got, _ := s.GetWorktree(context.Background(), "tower/feat-x")
	if got == nil {
		t.Fatal("not persisted")
	}
}

func TestAddWithFullBranchNamePassesThrough(t *testing.T) {
	g := &fakeGit{}
	svc, s := newSvc(t, g)
	w, err := svc.Add(context.Background(), "feature/sso")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if w.Branch != "feature/sso" {
		t.Errorf("branch: %q", w.Branch)
	}
	wantPath := filepath.Join("/repo", ".worktrees", "sso")
	if w.Path != wantPath {
		t.Errorf("path: want %q got %q", wantPath, w.Path)
	}
	got, _ := s.GetWorktree(context.Background(), "feature/sso")
	if got == nil {
		t.Fatal("not persisted")
	}
}

func TestAddRefusesIfWorktreeExists(t *testing.T) {
	g := &fakeGit{}
	svc, s := newSvc(t, g)
	now := time.Now().UTC().Truncate(time.Second)
	_ = s.UpsertWorktree(context.Background(), domain.Worktree{
		Branch: "tower/x", Path: "/wt", CreatedAt: now, LastSeen: now,
	})
	if _, err := svc.Add(context.Background(), "x"); err == nil {
		t.Fatal("expected error when worktree already tracked")
	}
	if g.added.path != "" {
		t.Errorf("git should not have been called: %+v", g.added)
	}
}

func TestAddGitFailureLeavesStoreUntouched(t *testing.T) {
	g := &fakeGit{addErr: errors.New("git failed")}
	svc, s := newSvc(t, g)
	_, err := svc.Add(context.Background(), "x")
	if err == nil {
		t.Fatal("expected error")
	}
	got, _ := s.GetWorktree(context.Background(), "tower/x")
	if got != nil {
		t.Errorf("store should be untouched: %+v", got)
	}
}

func TestRemoveTearsDownAndDeletes(t *testing.T) {
	g := &fakeGit{}
	svc, s := newSvc(t, g)
	now := time.Now().UTC().Truncate(time.Second)
	_ = s.UpsertWorktree(context.Background(), domain.Worktree{
		Branch: "tower/x", Path: "/repo/.worktrees/x", CreatedAt: now, LastSeen: now,
	})
	if err := svc.Remove(context.Background(), "x"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if g.removed != "/repo/.worktrees/x" {
		t.Errorf("git remove path: %q", g.removed)
	}
	got, _ := s.GetWorktree(context.Background(), "tower/x")
	if got != nil {
		t.Errorf("worktree should be deleted: %+v", got)
	}
}

func TestRemoveUnknown(t *testing.T) {
	svc, _ := newSvc(t, &fakeGit{})
	if err := svc.Remove(context.Background(), "missing"); err == nil {
		t.Fatal("expected error for unknown")
	}
}

func TestResolveShortAndFull(t *testing.T) {
	svc, s := newSvc(t, &fakeGit{})
	now := time.Now().UTC().Truncate(time.Second)
	_ = s.UpsertWorktree(context.Background(), domain.Worktree{
		Branch: "tower/x", Path: "/p", CreatedAt: now, LastSeen: now,
	})
	_ = s.UpsertWorktree(context.Background(), domain.Worktree{
		Branch: "feature/y", Path: "/q", CreatedAt: now, LastSeen: now,
	})
	w1, _ := svc.Resolve(context.Background(), "x")
	if w1 == nil || w1.Branch != "tower/x" {
		t.Fatalf("resolve short failed: %+v", w1)
	}
	w2, _ := svc.Resolve(context.Background(), "feature/y")
	if w2 == nil || w2.Branch != "feature/y" {
		t.Fatalf("resolve full failed: %+v", w2)
	}
}
