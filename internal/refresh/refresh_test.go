package refresh

import (
	"context"
	"errors"
	"os"
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
func (f *fakeGit) AddWorktree(_ context.Context, _, _ string) error         { return nil }
func (f *fakeGit) RemoveWorktree(_ context.Context, _ string, _ bool) error { return nil }
func (f *fakeGit) DeleteBranch(_ context.Context, _ string) error           { return nil }
func (f *fakeGit) Dirty(_ context.Context, p string) (bool, error)          { return f.dirty[p], nil }
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

// mustRepo registers a repo whose Path is a real directory under
// t.TempDir() so the path-existence check in ReconcileRepo passes.
// The fake git layer still serves whatever worktree paths the test
// configures — those don't need to exist.
func mustRepo(t *testing.T, s store.Store, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if err := s.UpsertRepo(context.Background(), domain.Repo{Name: name, Path: dir, CreatedAt: now}); err != nil {
		t.Fatalf("upsert repo: %v", err)
	}
	return dir
}

// gitFor returns a factory that always serves the given fake regardless of path.
func gitFor(g *fakeGit) GitFactory { return func(_ string) observe.Git { return g } }

// ghFor returns a factory that always serves the given fake regardless of path.
func ghFor(h *fakeGH) GHFactory { return func(_ string) observe.GH { return h } }

func TestReconcileAcrossRepos(t *testing.T) {
	s := newStore(t)
	oPath := mustRepo(t, s, "orchestra")
	rPath := mustRepo(t, s, "roxiq")

	gits := map[string]*fakeGit{
		oPath: {worktrees: []observe.Worktree{{Path: oPath, HEAD: "1", Branch: "main"}}},
		rPath: {worktrees: []observe.Worktree{
			{Path: rPath, HEAD: "2", Branch: "main"},
			{Path: filepath.Join(rPath, ".worktrees", "feat"), HEAD: "3", Branch: "tower/feat"},
		}},
	}
	svc := New(s, func(p string) observe.Git { return gits[p] }, ghFor(&fakeGH{}))

	if err := svc.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	all, _ := s.ListWorktrees(context.Background())
	if len(all) != 3 {
		t.Fatalf("want 3 worktrees across repos, got %d", len(all))
	}
	o, _ := s.ListWorktreesForRepo(context.Background(), "orchestra")
	r, _ := s.ListWorktreesForRepo(context.Background(), "roxiq")
	if len(o) != 1 || len(r) != 2 {
		t.Fatalf("scoping wrong: orchestra=%d roxiq=%d", len(o), len(r))
	}
}

func TestReconcileRepoEnrichment(t *testing.T) {
	s := newStore(t)
	mustRepo(t, s, "o")
	wtX := "/o/.worktrees/x" // arbitrary string the fake serves; not stat'd
	g := &fakeGit{
		worktrees:  []observe.Worktree{{Path: wtX, HEAD: "h", Branch: "tower/x"}},
		dirty:      map[string]bool{wtX: true},
		ahead:      map[string]int{wtX: 4},
		behind:     map[string]int{wtX: 1},
		lastCommit: map[string]time.Time{wtX: time.Unix(1700000000, 0).UTC()},
		title:      map[string]string{wtX: "wip"},
	}
	svc := New(s, gitFor(g), ghFor(&fakeGH{}))
	if err := svc.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	got, _ := s.GetWorktree(context.Background(), "o", "tower/x")
	if got == nil || !got.Dirty || got.Ahead != 4 || got.Behind != 1 || got.Title != "wip" {
		t.Fatalf("enrichment lost: %+v", got)
	}
}

func TestReconcileRepoDeletesStale(t *testing.T) {
	s := newStore(t)
	mustRepo(t, s, "o")
	now := time.Now().UTC().Truncate(time.Second)
	_ = s.UpsertWorktree(context.Background(), domain.Worktree{
		Repo: "o", Branch: "tower/old", Path: "/o/.worktrees/old", CreatedAt: now, LastSeen: now,
	})
	g := &fakeGit{worktrees: []observe.Worktree{{Path: "/o", HEAD: "h", Branch: "main"}}}
	svc := New(s, gitFor(g), ghFor(&fakeGH{}))
	if err := svc.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	got, _ := s.GetWorktree(context.Background(), "o", "tower/old")
	if got != nil {
		t.Errorf("stale should be deleted: %+v", got)
	}
}

func TestBranchScopesRepoOnRecords(t *testing.T) {
	s := newStore(t)
	mustRepo(t, s, "o")
	now := time.Now().UTC().Truncate(time.Second)
	_ = s.UpsertWorktree(context.Background(), domain.Worktree{
		Repo: "o", Branch: "tower/x", Path: "/p", CreatedAt: now, LastSeen: now,
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
	svc := New(s, gitFor(&fakeGit{}), ghFor(gh))
	if err := svc.Branch(context.Background(), "o", "tower/x"); err != nil {
		t.Fatalf("branch: %v", err)
	}
	pr, _ := s.GetPullRequest(context.Background(), "o", "tower/x")
	if pr == nil || pr.Repo != "o" || pr.Number != 99 {
		t.Fatalf("pr mismatch: %+v", pr)
	}
	revs, _ := s.ListReviews(context.Background(), "o", 99)
	if len(revs) != 1 || revs[0].Repo != "o" {
		t.Fatalf("review repo not scoped: %+v", revs)
	}
	checks, _ := s.ListCIChecks(context.Background(), "o", 99)
	if len(checks) != 1 || checks[0].Repo != "o" {
		t.Fatalf("check repo not scoped: %+v", checks)
	}
}

// One stale registration (path missing on disk) shouldn't block the
// reconcile of every other repo. The error is reported but the live
// repo's worktrees still land in the store.
func TestReconcileSkipsMissingRepoButStillReconcilesOthers(t *testing.T) {
	s := newStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	if err := s.UpsertRepo(context.Background(), domain.Repo{
		Name: "ghost", Path: "/definitely/not/here", CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	livePath := mustRepo(t, s, "live")

	gits := map[string]*fakeGit{
		livePath: {worktrees: []observe.Worktree{{Path: livePath, HEAD: "1", Branch: "main"}}},
	}
	svc := New(s, func(p string) observe.Git {
		if g, ok := gits[p]; ok {
			return g
		}
		return &fakeGit{worktreeErr: errors.New("unreachable: stat should have caught it first")}
	}, ghFor(&fakeGH{}))

	err := svc.Reconcile(context.Background())
	if err == nil {
		t.Fatal("want error reporting the missing repo, got nil")
	}
	if !contains(err.Error(), "ghost") || !contains(err.Error(), "missing on disk") {
		t.Fatalf("error should name the missing repo: %v", err)
	}
	if !contains(err.Error(), "tower repo prune") {
		t.Fatalf("error should hint at prune: %v", err)
	}

	// Live repo should still be reconciled.
	worktrees, _ := s.ListWorktreesForRepo(context.Background(), "live")
	if len(worktrees) != 1 {
		t.Fatalf("live repo should be reconciled despite ghost failure; got %d worktrees", len(worktrees))
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
