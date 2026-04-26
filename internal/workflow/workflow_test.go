package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/itsHabib/tower/internal/domain"
	"github.com/itsHabib/tower/internal/observe"
	"github.com/itsHabib/tower/internal/refresh"
	"github.com/itsHabib/tower/internal/store"
)

type fakeGit struct {
	added struct {
		path, branch string
	}
	removed string
	addErr  error
	rmErr   error
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
	if f.rmErr != nil {
		return f.rmErr
	}
	f.removed = path
	return nil
}

type fakeGH struct{}

func (fakeGH) PullRequestForBranch(context.Context, string) (*domain.PullRequest, error) {
	return nil, nil
}
func (fakeGH) Reviews(context.Context, int) ([]domain.Review, error) { return nil, nil }
func (fakeGH) Checks(context.Context, int) ([]domain.CICheck, error) { return nil, nil }

func newSvc(t *testing.T, git observe.Git) (*Service, store.Store) {
	t.Helper()
	s, err := store.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ref := refresh.New(s, fakeGH{})
	svc := New(Config{Repo: "/repo"}, s, git, ref)
	svc.now = func() time.Time { return time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC) }
	return svc, s
}

func TestAddCreatesWorktreeAndFlipsStatus(t *testing.T) {
	g := &fakeGit{}
	svc, s := newSvc(t, g)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	if err := s.UpsertTask(ctx, domain.Task{
		ID: "feat-x", Title: "Feature X", Path: "/p", Status: domain.StatusDraft,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed task: %v", err)
	}

	if err := svc.Add(ctx, "feat-x"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if g.added.path != filepath.Join("/repo", ".worktrees", "feat-x") {
		t.Errorf("path: got %q", g.added.path)
	}
	if g.added.branch != "tower/feat-x" {
		t.Errorf("branch: got %q", g.added.branch)
	}
	wt, _ := s.GetWorktree(ctx, "feat-x")
	if wt == nil || wt.Branch != "tower/feat-x" {
		t.Fatalf("worktree not persisted: %+v", wt)
	}
	got, _ := s.GetTask(ctx, "feat-x")
	if got.Status != domain.StatusActive {
		t.Errorf("status: got %s", got.Status)
	}
}

func TestAddRefusesIfWorktreeExists(t *testing.T) {
	g := &fakeGit{}
	svc, s := newSvc(t, g)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	_ = s.UpsertTask(ctx, domain.Task{ID: "x", Title: "X", Path: "/p", Status: domain.StatusActive, CreatedAt: now, UpdatedAt: now})
	_ = s.SetWorktree(ctx, domain.Worktree{TaskID: "x", Path: "/wt", Branch: "tower/x", CreatedAt: now})

	if err := svc.Add(ctx, "x"); err == nil {
		t.Fatal("expected error when worktree exists")
	}
	if g.added.path != "" {
		t.Errorf("git should not have been called: %+v", g.added)
	}
}

func TestAddMissingTask(t *testing.T) {
	svc, _ := newSvc(t, &fakeGit{})
	if err := svc.Add(context.Background(), "missing"); err == nil {
		t.Fatal("expected error for missing task")
	}
}

func TestAddGitFailureRollbackNotAttempted(t *testing.T) {
	g := &fakeGit{addErr: errors.New("worktree add failed")}
	svc, s := newSvc(t, g)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	_ = s.UpsertTask(ctx, domain.Task{ID: "x", Title: "X", Path: "/p", Status: domain.StatusDraft, CreatedAt: now, UpdatedAt: now})

	if err := svc.Add(ctx, "x"); err == nil {
		t.Fatal("expected error from git failure")
	}
	wt, _ := s.GetWorktree(ctx, "x")
	if wt != nil {
		t.Errorf("worktree should not be persisted on git failure: %+v", wt)
	}
	got, _ := s.GetTask(ctx, "x")
	if got.Status != domain.StatusDraft {
		t.Errorf("status should remain draft on git failure, got %s", got.Status)
	}
}

func TestRemoveTearsDownAndAbandons(t *testing.T) {
	g := &fakeGit{}
	svc, s := newSvc(t, g)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	_ = s.UpsertTask(ctx, domain.Task{ID: "x", Title: "X", Path: "/p", Status: domain.StatusActive, CreatedAt: now, UpdatedAt: now})
	_ = s.SetWorktree(ctx, domain.Worktree{TaskID: "x", Path: "/repo/.worktrees/x", Branch: "tower/x", CreatedAt: now})

	if err := svc.Remove(ctx, "x"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if g.removed != "/repo/.worktrees/x" {
		t.Errorf("git remove path: got %q", g.removed)
	}
	wt, _ := s.GetWorktree(ctx, "x")
	if wt != nil {
		t.Errorf("worktree should be deleted: %+v", wt)
	}
	got, _ := s.GetTask(ctx, "x")
	if got.Status != domain.StatusAbandoned {
		t.Errorf("status: got %s", got.Status)
	}
}

func TestRemoveTaskWithNoWorktree(t *testing.T) {
	g := &fakeGit{}
	svc, s := newSvc(t, g)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	_ = s.UpsertTask(ctx, domain.Task{ID: "x", Title: "X", Path: "/p", Status: domain.StatusDraft, CreatedAt: now, UpdatedAt: now})

	if err := svc.Remove(ctx, "x"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if g.removed != "" {
		t.Errorf("git should not have been called: %q", g.removed)
	}
	got, _ := s.GetTask(ctx, "x")
	if got.Status != domain.StatusAbandoned {
		t.Errorf("status: got %s", got.Status)
	}
}

func TestDiscoverAddsAndPreservesStatus(t *testing.T) {
	g := &fakeGit{}
	svc, s := newSvc(t, g)
	ctx := context.Background()
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "feat-a.md"), []byte("# Feature A\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "feat-b.md"), []byte("---\nid: feat-b\nstatus: active\n---\n# B"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := svc.Discover(ctx, dir)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if res.Added != 2 || res.Updated != 0 {
		t.Errorf("first scan: want added=2 updated=0, got added=%d updated=%d", res.Added, res.Updated)
	}

	a, _ := s.GetTask(ctx, "feat-a")
	if a.Status != domain.StatusDraft {
		t.Errorf("feat-a: want draft, got %s", a.Status)
	}
	a.Status = domain.StatusBlocked
	if err := s.UpsertTask(ctx, *a); err != nil {
		t.Fatal(err)
	}

	res, err = svc.Discover(ctx, dir)
	if err != nil {
		t.Fatalf("discover 2: %v", err)
	}
	if res.Added != 0 || res.Updated != 2 {
		t.Errorf("second scan: want added=0 updated=2, got added=%d updated=%d", res.Added, res.Updated)
	}

	a, _ = s.GetTask(ctx, "feat-a")
	if a.Status != domain.StatusBlocked {
		t.Errorf("feat-a status should be preserved as blocked, got %s", a.Status)
	}
}
