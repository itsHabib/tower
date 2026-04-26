package mcp

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/itsHabib/tower/internal/domain"
	"github.com/itsHabib/tower/internal/observe"
	"github.com/itsHabib/tower/internal/refresh"
	"github.com/itsHabib/tower/internal/store"
	"github.com/itsHabib/tower/internal/workflow"
)

type fakeGit struct {
	worktrees []observe.Worktree
}

func (f *fakeGit) Worktrees(context.Context) ([]observe.Worktree, error) { return f.worktrees, nil }
func (*fakeGit) AddWorktree(context.Context, string, string) error       { return nil }
func (*fakeGit) RemoveWorktree(context.Context, string) error            { return nil }
func (*fakeGit) Dirty(context.Context, string) (bool, error)             { return false, nil }
func (*fakeGit) AheadBehind(context.Context, string) (int, int, error)   { return 0, 0, nil }
func (*fakeGit) LastCommit(context.Context, string) (time.Time, string, error) {
	return time.Time{}, "", nil
}
func (*fakeGit) MainRoot(context.Context) (string, error) { return "", nil }

type fakeGH struct{}

func (fakeGH) PullRequestForBranch(context.Context, string) (*domain.PullRequest, error) {
	return nil, nil
}
func (fakeGH) Reviews(context.Context, int) ([]domain.Review, error) { return nil, nil }
func (fakeGH) Checks(context.Context, int) ([]domain.CICheck, error) { return nil, nil }

func newTestHandlers(t *testing.T) *handlers {
	t.Helper()
	ctx := context.Background()
	s, err := store.OpenSQLite(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	git := &fakeGit{}
	gitFactory := func(string) observe.Git { return git }
	ghFactory := func(string) observe.GH { return fakeGH{} }
	ref := refresh.New(s, gitFactory, ghFactory)
	wf := workflow.New(workflow.Config{}, s, gitFactory, ref)
	return &handlers{workflow: wf, store: s}
}

func TestListWorktreesAllAndFiltered(t *testing.T) {
	h := newTestHandlers(t)
	ctx := context.Background()
	if _, err := h.workflow.AddRepo(ctx, t.TempDir(), "alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.workflow.AddRepo(ctx, t.TempDir(), "beta"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.workflow.Add(ctx, "alpha", "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.workflow.Add(ctx, "beta", "y"); err != nil {
		t.Fatal(err)
	}

	all, err := h.callListWorktrees(ctx, ListWorktreesArgs{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Errorf("want 2 worktrees across both repos, got %d", len(all))
	}

	scoped, err := h.callListWorktrees(ctx, ListWorktreesArgs{Repo: "alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if len(scoped) != 1 || scoped[0].Worktree.Repo != "alpha" {
		t.Errorf("repo filter wrong: %+v", scoped)
	}
	if scoped[0].Reviews == nil || scoped[0].Checks == nil {
		t.Errorf("reviews/checks must be non-nil for downstream consumers: %+v", scoped[0])
	}
}

func TestGetWorktreeByBranchAndByName(t *testing.T) {
	h := newTestHandlers(t)
	ctx := context.Background()
	if _, err := h.workflow.AddRepo(ctx, t.TempDir(), "myrepo"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.workflow.Add(ctx, "myrepo", "feat-x"); err != nil {
		t.Fatal(err)
	}

	byBranch, errResult, err := h.callGetWorktree(ctx, GetWorktreeArgs{Repo: "myrepo", Branch: "tower/feat-x"})
	if err != nil || errResult != nil {
		t.Fatalf("byBranch: err=%v errResult=%+v", err, errResult)
	}
	if byBranch.Worktree.Branch != "tower/feat-x" {
		t.Errorf("branch: %q", byBranch.Worktree.Branch)
	}

	byName, errResult, err := h.callGetWorktree(ctx, GetWorktreeArgs{Name: "feat-x"})
	if err != nil || errResult != nil {
		t.Fatalf("byName: err=%v errResult=%+v", err, errResult)
	}
	if byName.Worktree.Branch != "tower/feat-x" {
		t.Errorf("name resolve: %q", byName.Worktree.Branch)
	}
}

func TestGetWorktreeNotFoundReturnsToolError(t *testing.T) {
	h := newTestHandlers(t)
	ctx := context.Background()
	if _, err := h.workflow.AddRepo(ctx, t.TempDir(), "myrepo"); err != nil {
		t.Fatal(err)
	}

	_, errResult, err := h.callGetWorktree(ctx, GetWorktreeArgs{Repo: "myrepo", Branch: "tower/nope"})
	if err != nil {
		t.Fatalf("expected tool error not protocol error: %v", err)
	}
	if errResult == nil || !errResult.IsError {
		t.Errorf("expected IsError result for missing worktree, got %+v", errResult)
	}
}

func TestAddWorktreeCreatesAndPersists(t *testing.T) {
	h := newTestHandlers(t)
	ctx := context.Background()
	if _, err := h.workflow.AddRepo(ctx, t.TempDir(), "myrepo"); err != nil {
		t.Fatal(err)
	}

	wt, errResult, err := h.callAddWorktree(ctx, AddWorktreeArgs{Repo: "myrepo", Name: "feat-x"})
	if err != nil || errResult != nil {
		t.Fatalf("add: err=%v errResult=%+v", err, errResult)
	}
	if wt.Branch != "tower/feat-x" {
		t.Errorf("branch: %q", wt.Branch)
	}

	got, _ := h.store.GetWorktree(ctx, "myrepo", "tower/feat-x")
	if got == nil {
		t.Errorf("not persisted")
	}
}

func TestRemoveWorktreeDeletes(t *testing.T) {
	h := newTestHandlers(t)
	ctx := context.Background()
	if _, err := h.workflow.AddRepo(ctx, t.TempDir(), "myrepo"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.workflow.Add(ctx, "myrepo", "feat-x"); err != nil {
		t.Fatal(err)
	}

	res, errResult, err := h.callRemoveWorktree(ctx, RemoveWorktreeArgs{Repo: "myrepo", Name: "feat-x"})
	if err != nil || errResult != nil {
		t.Fatalf("remove: err=%v errResult=%+v", err, errResult)
	}
	if !res.Removed {
		t.Errorf("expected Removed=true: %+v", res)
	}
	got, _ := h.store.GetWorktree(ctx, "myrepo", "tower/feat-x")
	if got != nil {
		t.Errorf("worktree should be gone: %+v", got)
	}
}

func TestSyncReportsCount(t *testing.T) {
	h := newTestHandlers(t)
	ctx := context.Background()
	repoPath := t.TempDir()
	if _, err := h.workflow.AddRepo(ctx, repoPath, "myrepo"); err != nil {
		t.Fatal(err)
	}

	res, _, err := h.callSync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Synced != 0 || len(res.Errors) != 0 {
		t.Errorf("empty repo should sync 0 with no errors: %+v", res)
	}
}

func TestListRepos(t *testing.T) {
	h := newTestHandlers(t)
	ctx := context.Background()
	if _, err := h.workflow.AddRepo(ctx, t.TempDir(), "alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.workflow.AddRepo(ctx, t.TempDir(), "beta"); err != nil {
		t.Fatal(err)
	}

	repos, _, err := h.callListRepos(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 2 {
		t.Errorf("want 2 repos, got %d", len(repos))
	}
}

func TestReconcileSweepsRegisteredRepos(t *testing.T) {
	h := newTestHandlers(t)
	ctx := context.Background()
	if _, err := h.workflow.AddRepo(ctx, t.TempDir(), "myrepo"); err != nil {
		t.Fatal(err)
	}

	res, _, err := h.callReconcile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK {
		t.Errorf("want OK=true: %+v", res)
	}
}

func TestRegisterRepoRequiresPath(t *testing.T) {
	h := newTestHandlers(t)
	ctx := context.Background()

	_, errResult, err := h.callRegisterRepo(ctx, RegisterRepoArgs{Name: "x"})
	if err != nil {
		t.Fatalf("want tool error not protocol error: %v", err)
	}
	if errResult == nil || !errResult.IsError {
		t.Errorf("missing path should produce IsError result, got %+v", errResult)
	}
}

func TestRegisterRepoPersists(t *testing.T) {
	h := newTestHandlers(t)
	ctx := context.Background()
	repoPath := t.TempDir()

	r, errResult, err := h.callRegisterRepo(ctx, RegisterRepoArgs{Path: repoPath, Name: "myrepo"})
	if err != nil || errResult != nil {
		t.Fatalf("register: err=%v errResult=%+v", err, errResult)
	}
	if r.Name != "myrepo" || r.Path != repoPath {
		t.Errorf("repo: %+v", r)
	}
	got, _ := h.store.GetRepo(ctx, "myrepo")
	if got == nil {
		t.Errorf("not persisted")
	}
}

func TestUnregisterRepoDeletes(t *testing.T) {
	h := newTestHandlers(t)
	ctx := context.Background()
	if _, err := h.workflow.AddRepo(ctx, t.TempDir(), "myrepo"); err != nil {
		t.Fatal(err)
	}

	res, errResult, err := h.callUnregisterRepo(ctx, UnregisterRepoArgs{Name: "myrepo"})
	if err != nil || errResult != nil {
		t.Fatalf("unregister: err=%v errResult=%+v", err, errResult)
	}
	if !res.Unregistered {
		t.Errorf("expected Unregistered=true: %+v", res)
	}
	got, _ := h.store.GetRepo(ctx, "myrepo")
	if got != nil {
		t.Errorf("repo should be gone: %+v", got)
	}
}

func TestPruneReposRemovesMissingPaths(t *testing.T) {
	h := newTestHandlers(t)
	ctx := context.Background()
	// Register one real repo and one ghost (path doesn't exist).
	live := t.TempDir()
	if _, err := h.workflow.AddRepo(ctx, live, "live"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.workflow.AddRepo(ctx, "/definitely/not/a/real/path/tower-mcp-test", "ghost"); err != nil {
		t.Fatal(err)
	}

	// Dry run reports only.
	dry, _, err := h.callPruneRepos(ctx, PruneReposArgs{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !dry.DryRun || len(dry.Pruned) != 1 || dry.Pruned[0] != "ghost" {
		t.Errorf("dry run: %+v", dry)
	}
	got, _ := h.store.GetRepo(ctx, "ghost")
	if got == nil {
		t.Errorf("dry run should not have removed: %+v", got)
	}

	// Live run removes.
	live2res, _, err := h.callPruneRepos(ctx, PruneReposArgs{})
	if err != nil {
		t.Fatal(err)
	}
	if live2res.DryRun || len(live2res.Pruned) != 1 || live2res.Pruned[0] != "ghost" {
		t.Errorf("live run: %+v", live2res)
	}
	got, _ = h.store.GetRepo(ctx, "ghost")
	if got != nil {
		t.Errorf("ghost should be gone: %+v", got)
	}
	live2, _ := h.store.GetRepo(ctx, "live")
	if live2 == nil {
		t.Errorf("live repo should be untouched")
	}
}

// Test shims call handlers without the SDK's request wrapper since
// the handlers don't read req.Params for their own logic.

func (h *handlers) callListWorktrees(ctx context.Context, args ListWorktreesArgs) ([]domain.WorktreeView, error) {
	_, out, err := h.listWorktrees(ctx, nil, args)
	return out.Worktrees, err
}

func (h *handlers) callGetWorktree(ctx context.Context, args GetWorktreeArgs) (*domain.WorktreeView, *mcp.CallToolResult, error) {
	res, out, err := h.getWorktree(ctx, nil, args)
	return out, res, err
}

func (h *handlers) callAddWorktree(ctx context.Context, args AddWorktreeArgs) (*domain.Worktree, *mcp.CallToolResult, error) {
	res, out, err := h.addWorktree(ctx, nil, args)
	return out, res, err
}

func (h *handlers) callRemoveWorktree(ctx context.Context, args RemoveWorktreeArgs) (RemoveResult, *mcp.CallToolResult, error) {
	res, out, err := h.removeWorktree(ctx, nil, args)
	return out, res, err
}

func (h *handlers) callSync(ctx context.Context) (SyncResult, *mcp.CallToolResult, error) {
	res, out, err := h.sync(ctx, nil, SyncArgs{})
	return out, res, err
}

func (h *handlers) callListRepos(ctx context.Context) ([]domain.Repo, *mcp.CallToolResult, error) {
	res, out, err := h.listRepos(ctx, nil, ListReposArgs{})
	return out.Repos, res, err
}

func (h *handlers) callReconcile(ctx context.Context) (ReconcileResult, *mcp.CallToolResult, error) {
	res, out, err := h.reconcile(ctx, nil, ReconcileArgs{})
	return out, res, err
}

func (h *handlers) callRegisterRepo(ctx context.Context, args RegisterRepoArgs) (*domain.Repo, *mcp.CallToolResult, error) {
	res, out, err := h.registerRepo(ctx, nil, args)
	return out, res, err
}

func (h *handlers) callUnregisterRepo(ctx context.Context, args UnregisterRepoArgs) (UnregisterResult, *mcp.CallToolResult, error) {
	res, out, err := h.unregisterRepo(ctx, nil, args)
	return out, res, err
}

func (h *handlers) callPruneRepos(ctx context.Context, args PruneReposArgs) (PruneReposResult, *mcp.CallToolResult, error) {
	res, out, err := h.pruneRepos(ctx, nil, args)
	return out, res, err
}
