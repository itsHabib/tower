package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/itsHabib/tower/internal/domain"
	"github.com/itsHabib/tower/internal/observe"
	"github.com/itsHabib/tower/internal/refresh"
	"github.com/itsHabib/tower/internal/store"
	"github.com/itsHabib/tower/internal/workflow"
)

type fakeGit struct {
	worktrees    []observe.Worktree
	addedPath    string
	addedBranch  string
	removedPaths []string
}

func (f *fakeGit) Worktrees(context.Context) ([]observe.Worktree, error) { return f.worktrees, nil }
func (f *fakeGit) AddWorktree(_ context.Context, path, branch string) error {
	f.addedPath, f.addedBranch = path, branch
	return nil
}
func (f *fakeGit) RemoveWorktree(_ context.Context, path string) error {
	f.removedPaths = append(f.removedPaths, path)
	return nil
}
func (*fakeGit) Dirty(context.Context, string) (bool, error)           { return false, nil }
func (*fakeGit) AheadBehind(context.Context, string) (int, int, error) { return 0, 0, nil }
func (*fakeGit) LastCommit(context.Context, string) (time.Time, string, error) {
	return time.Time{}, "", nil
}
func (*fakeGit) MainRoot(context.Context) (string, error) { return "", nil }

type fakeGH struct {
	prByBranch map[string]*domain.PullRequest
	reviews    map[int][]domain.Review
	checks     map[int][]domain.CICheck
}

func (f *fakeGH) PullRequestForBranch(_ context.Context, branch string) (*domain.PullRequest, error) {
	pr, ok := f.prByBranch[branch]
	if !ok {
		return nil, nil
	}
	cp := *pr
	return &cp, nil
}
func (f *fakeGH) Reviews(_ context.Context, n int) ([]domain.Review, error) { return f.reviews[n], nil }
func (f *fakeGH) Checks(_ context.Context, n int) ([]domain.CICheck, error) { return f.checks[n], nil }

// testEnv bundles a fully-wired test cliCtx with handles to the fakes
// behind it, so tests can pre-program responses or assert on calls.
type testEnv struct {
	c   *cliCtx
	git *fakeGit
	gh  *fakeGH
}

func newTestEnv(t *testing.T) testEnv {
	t.Helper()
	ctx := context.Background()
	s, err := store.OpenSQLite(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	git := &fakeGit{}
	gh := &fakeGH{}
	gitFactory := func(string) observe.Git { return git }
	ghFactory := func(string) observe.GH { return gh }
	ref := refresh.New(s, gitFactory, ghFactory)
	wf := workflow.New(workflow.Config{}, s, gitFactory, ref)
	return testEnv{
		c:   &cliCtx{store: s, workflow: wf},
		git: git,
		gh:  gh,
	}
}
