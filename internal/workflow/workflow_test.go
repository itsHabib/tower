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
	added struct {
		path, branch string
		repoPath     string
	}
	removed         string
	removedForce    bool
	deletedBranches []string
	addErr          error
	delBranchErr    error
}

func (f *fakeGit) Worktrees(_ context.Context) ([]observe.Worktree, error) { return nil, nil }
func (f *fakeGit) AddWorktree(_ context.Context, path, branch string) error {
	if f.addErr != nil {
		return f.addErr
	}
	f.added.path, f.added.branch = path, branch
	return nil
}
func (f *fakeGit) RemoveWorktree(_ context.Context, path string, force bool) error {
	f.removed = path
	f.removedForce = force
	return nil
}
func (f *fakeGit) DeleteBranch(_ context.Context, branch string) error {
	if f.delBranchErr != nil {
		return f.delBranchErr
	}
	f.deletedBranches = append(f.deletedBranches, branch)
	return nil
}
func (f *fakeGit) Dirty(_ context.Context, _ string) (bool, error)           { return false, nil }
func (f *fakeGit) AheadBehind(_ context.Context, _ string) (int, int, error) { return 0, 0, nil }
func (f *fakeGit) LastCommit(_ context.Context, _ string) (time.Time, string, error) {
	return time.Time{}, "", nil
}
func (f *fakeGit) MainRoot(_ context.Context) (string, error) { return "", nil }

type fakeGH struct{}

func (fakeGH) PullRequestForBranch(context.Context, string) (*domain.PullRequest, error) {
	return nil, nil
}
func (fakeGH) Reviews(context.Context, int) ([]domain.Review, error) { return nil, nil }
func (fakeGH) Checks(context.Context, int) ([]domain.CICheck, error) { return nil, nil }

func newSvc(t *testing.T, git *fakeGit) (*Service, store.Store) {
	t.Helper()
	s, err := store.OpenSQLite(context.Background(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	gitFactory := func(p string) observe.Git {
		git.added.repoPath = p
		return git
	}
	ghFactory := func(_ string) observe.GH { return fakeGH{} }
	ref := refresh.New(s, gitFactory, ghFactory)
	svc := New(Config{}, s, gitFactory, ref)
	svc.now = func() time.Time { return time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC) }
	return svc, s
}

func mustRepo(t *testing.T, svc *Service, path string) {
	t.Helper()
	if _, err := svc.AddRepo(context.Background(), path, ""); err != nil {
		t.Fatalf("add repo: %v", err)
	}
}

func TestAddRepoUsesBasenameByDefault(t *testing.T) {
	svc, s := newSvc(t, &fakeGit{})
	r, err := svc.AddRepo(context.Background(), "/path/to/orchestra", "")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if r.Name != "orchestra" {
		t.Errorf("name: %q", r.Name)
	}
	got, _ := s.GetRepo(context.Background(), "orchestra")
	if got == nil {
		t.Fatal("not persisted")
	}
}

func TestAddRepoRejectsNameCollisionAtDifferentPath(t *testing.T) {
	svc, _ := newSvc(t, &fakeGit{})
	_, err := svc.AddRepo(context.Background(), "/a/foo", "")
	if err != nil {
		t.Fatalf("first add: %v", err)
	}
	_, err = svc.AddRepo(context.Background(), "/b/foo", "")
	if err == nil {
		t.Fatal("expected error for name collision at different path")
	}
}

func TestAddCreatesWorktreeInNamedRepo(t *testing.T) {
	g := &fakeGit{}
	svc, s := newSvc(t, g)
	repoPath, _ := filepath.Abs("/pers/orchestra")
	mustRepo(t, svc, repoPath)
	w, err := svc.Add(context.Background(), "orchestra", "feat-x")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	wantPath := filepath.Join(repoPath, ".worktrees", "feat-x")
	if w.Path != wantPath {
		t.Errorf("path: want %q got %q", wantPath, w.Path)
	}
	if w.Branch != "tower/feat-x" || w.Repo != "orchestra" {
		t.Errorf("worktree fields: %+v", w)
	}
	got, _ := s.GetWorktree(context.Background(), "orchestra", "tower/feat-x")
	if got == nil {
		t.Fatal("not persisted")
	}
}

func TestAddRequiresRegisteredRepo(t *testing.T) {
	svc, _ := newSvc(t, &fakeGit{})
	if _, err := svc.Add(context.Background(), "ghost", "x"); err == nil {
		t.Fatal("expected error for unregistered repo")
	}
}

func TestRemoveTearsDownInRepo(t *testing.T) {
	g := &fakeGit{}
	svc, s := newSvc(t, g)
	mustRepo(t, svc, "/pers/orchestra")
	if _, err := svc.Add(context.Background(), "orchestra", "x"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Remove(context.Background(), "orchestra", "x", false); err != nil {
		t.Fatalf("remove: %v", err)
	}
	got, _ := s.GetWorktree(context.Background(), "orchestra", "tower/x")
	if got != nil {
		t.Errorf("should be deleted: %+v", got)
	}
}

func TestRemovePassesForceToGit(t *testing.T) {
	g := &fakeGit{}
	svc, _ := newSvc(t, g)
	mustRepo(t, svc, "/pers/orchestra")
	if _, err := svc.Add(context.Background(), "orchestra", "x"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Remove(context.Background(), "orchestra", "x", true); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !g.removedForce {
		t.Fatalf("force=true was not propagated to git.RemoveWorktree")
	}
}

func TestRemoveDeletesBranchSoSameNameReAdds(t *testing.T) {
	g := &fakeGit{}
	svc, _ := newSvc(t, g)
	mustRepo(t, svc, "/pers/orchestra")
	if _, err := svc.Add(context.Background(), "orchestra", "x"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Remove(context.Background(), "orchestra", "x", false); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if len(g.deletedBranches) != 1 || g.deletedBranches[0] != "tower/x" {
		t.Fatalf("want branch tower/x deleted, got %v", g.deletedBranches)
	}
	if _, err := svc.Add(context.Background(), "orchestra", "x"); err != nil {
		t.Fatalf("re-add after remove failed: %v", err)
	}
}

func TestRemoveKeepsUnmergedBranchAndReportsIt(t *testing.T) {
	g := &fakeGit{delBranchErr: errors.New("not fully merged")}
	svc, s := newSvc(t, g)
	mustRepo(t, svc, "/pers/orchestra")
	if _, err := svc.Add(context.Background(), "orchestra", "x"); err != nil {
		t.Fatal(err)
	}
	err := svc.Remove(context.Background(), "orchestra", "x", false)
	if !errors.Is(err, ErrBranchKeptUnmerged) {
		t.Fatalf("want ErrBranchKeptUnmerged, got %v", err)
	}
	// Worktree row should still be cleaned out so the TUI doesn't show
	// a stale entry — only the branch ref lingers.
	got, _ := s.GetWorktree(context.Background(), "orchestra", "tower/x")
	if got != nil {
		t.Errorf("worktree row should be deleted: %+v", got)
	}
}

func TestResolveAcrossReposAmbiguous(t *testing.T) {
	g := &fakeGit{}
	svc, s := newSvc(t, g)
	mustRepo(t, svc, "/pers/orchestra")
	mustRepo(t, svc, "/pers/roxiq")
	now := time.Now().UTC().Truncate(time.Second)
	_ = s.UpsertWorktree(context.Background(), domain.Worktree{
		Repo: "orchestra", Branch: "tower/x", Path: "/o/x", CreatedAt: now, LastSeen: now,
	})
	_ = s.UpsertWorktree(context.Background(), domain.Worktree{
		Repo: "roxiq", Branch: "tower/x", Path: "/r/x", CreatedAt: now, LastSeen: now,
	})
	_, err := svc.Resolve(context.Background(), "", "x")
	if !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("want ErrAmbiguous, got %v", err)
	}
	got, err := svc.Resolve(context.Background(), "orchestra", "x")
	if err != nil || got == nil || got.Path != "/o/x" {
		t.Fatalf("scoped resolve: %v %+v", err, got)
	}
}

func TestRepoForPath(t *testing.T) {
	svc, _ := newSvc(t, &fakeGit{})
	mustRepo(t, svc, "/pers/orchestra")
	mustRepo(t, svc, "/pers/roxiq")
	r, err := svc.RepoForPath(context.Background(), "/pers/orchestra/internal/dag")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if r == nil || r.Name != "orchestra" {
		t.Fatalf("want orchestra, got %+v", r)
	}
	r, err = svc.RepoForPath(context.Background(), "/pers/something-else")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if r != nil {
		t.Fatalf("want nil, got %+v", r)
	}
}
