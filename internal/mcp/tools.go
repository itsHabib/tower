package mcp

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/itsHabib/tower/internal/domain"
	"github.com/itsHabib/tower/internal/store"
)

func (h *handlers) register(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_worktrees",
		Description: "List tracked worktrees across registered repos, each with its PR / reviews / CI state. Optionally filter to one repo.",
	}, h.listWorktrees)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_worktree",
		Description: "Get one worktree (with PR / reviews / CI). Provide either {repo, branch} for an exact lookup or {name} (with optional repo) to resolve by short name.",
	}, h.getWorktree)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "add_worktree",
		Description: "Create a new tower-style worktree in the named repo. The branch becomes tower/<name> and the path becomes <repo>/.worktrees/<name>.",
	}, h.addWorktree)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "remove_worktree",
		Description: "Remove a tracked worktree (git worktree remove + drop from tower's store). Refuses to remove the main worktree of a repo.",
	}, h.removeWorktree)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "sync",
		Description: "Reconcile worktrees from git, then refresh PR / reviews / CI from GitHub for every tracked branch. Returns counts and any per-branch errors.",
	}, h.sync)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "reconcile",
		Description: "Reconcile worktrees from local git only — no GitHub network calls. Cheaper than sync; use when you only care about which worktrees exist and their dirty / ahead-behind state.",
	}, h.reconcile)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_repos",
		Description: "List every registered repo (name + filesystem path).",
	}, h.listRepos)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "register_repo",
		Description: "Register a repo so tower starts tracking its worktrees. Path is required; the MCP server has no cwd context. Name defaults to the directory basename.",
	}, h.registerRepo)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "unregister_repo",
		Description: "Unregister a repo. Cascades to its tracked worktrees, PRs, reviews, and CI checks in tower's store. Does not touch the worktrees on disk.",
	}, h.unregisterRepo)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "prune_repos",
		Description: "Unregister repos whose path no longer exists on disk. Pass dry_run to report without removing.",
	}, h.pruneRepos)
}

// ListWorktreesArgs is the input for list_worktrees.
type ListWorktreesArgs struct {
	Repo string `json:"repo,omitempty" jsonschema:"if set, list worktrees in this repo only; otherwise list across every registered repo"`
}

// ListWorktreesResult wraps the array because MCP tool outputs must be
// JSON objects, not bare arrays.
type ListWorktreesResult struct {
	Worktrees []domain.WorktreeView `json:"worktrees"`
}

func (h *handlers) listWorktrees(ctx context.Context, _ *mcp.CallToolRequest, args ListWorktreesArgs) (*mcp.CallToolResult, ListWorktreesResult, error) {
	var (
		wts []domain.Worktree
		err error
	)
	if args.Repo == "" {
		wts, err = h.store.ListWorktrees(ctx)
	} else {
		wts, err = h.store.ListWorktreesForRepo(ctx, args.Repo)
	}
	if err != nil {
		return nil, ListWorktreesResult{}, err
	}
	out := make([]domain.WorktreeView, 0, len(wts))
	for _, wt := range wts {
		view, err := buildView(ctx, h.store, wt)
		if err != nil {
			return nil, ListWorktreesResult{}, err
		}
		out = append(out, view)
	}
	return nil, ListWorktreesResult{Worktrees: out}, nil
}

// GetWorktreeArgs is the input for get_worktree. Either {repo, branch}
// or {name} (with optional repo) is required.
type GetWorktreeArgs struct {
	Repo   string `json:"repo,omitempty" jsonschema:"repo name. With branch, looks up exactly. With name, scopes resolution."`
	Branch string `json:"branch,omitempty" jsonschema:"full branch name (e.g. tower/feat-x). Requires repo."`
	Name   string `json:"name,omitempty" jsonschema:"short name; resolved via the same rules as 'tower open <name>'"`
}

func (h *handlers) getWorktree(ctx context.Context, _ *mcp.CallToolRequest, args GetWorktreeArgs) (*mcp.CallToolResult, *domain.WorktreeView, error) {
	var wt *domain.Worktree
	switch {
	case args.Branch != "" && args.Repo != "":
		var err error
		wt, err = h.store.GetWorktree(ctx, args.Repo, args.Branch)
		if err != nil {
			return nil, nil, err
		}
	case args.Name != "":
		var err error
		wt, err = h.workflow.Resolve(ctx, args.Repo, args.Name)
		if err != nil {
			return toolError("resolve %q: %v", args.Name, err), nil, nil
		}
	default:
		return toolError("provide either {repo, branch} or {name}"), nil, nil
	}
	if wt == nil {
		return toolError("worktree not found"), nil, nil
	}
	view, err := buildView(ctx, h.store, *wt)
	if err != nil {
		return nil, nil, err
	}
	return nil, &view, nil
}

// AddWorktreeArgs is the input for add_worktree.
type AddWorktreeArgs struct {
	Repo string `json:"repo" jsonschema:"name of an already-registered repo"`
	Name string `json:"name" jsonschema:"worktree short name; becomes branch tower/<name>"`
}

func (h *handlers) addWorktree(ctx context.Context, _ *mcp.CallToolRequest, args AddWorktreeArgs) (*mcp.CallToolResult, *domain.Worktree, error) {
	if args.Repo == "" || args.Name == "" {
		return toolError("repo and name are both required"), nil, nil
	}
	wt, err := h.workflow.Add(ctx, args.Repo, args.Name)
	if err != nil {
		return toolError("add: %v", err), nil, nil
	}
	return nil, wt, nil
}

// RemoveWorktreeArgs is the input for remove_worktree.
type RemoveWorktreeArgs struct {
	Repo string `json:"repo" jsonschema:"name of the registered repo"`
	Name string `json:"name" jsonschema:"worktree short name (or full branch with a slash)"`
}

// RemoveResult confirms the deletion. Plain shape so the agent can
// verify success without re-reading the worktree list.
type RemoveResult struct {
	Removed bool   `json:"removed"`
	Repo    string `json:"repo"`
	Name    string `json:"name"`
}

func (h *handlers) removeWorktree(ctx context.Context, _ *mcp.CallToolRequest, args RemoveWorktreeArgs) (*mcp.CallToolResult, RemoveResult, error) {
	if args.Repo == "" || args.Name == "" {
		return toolError("repo and name are both required"), RemoveResult{}, nil
	}
	if err := h.workflow.Remove(ctx, args.Repo, args.Name); err != nil {
		return toolError("remove: %v", err), RemoveResult{}, nil
	}
	return nil, RemoveResult{Removed: true, Repo: args.Repo, Name: args.Name}, nil
}

// SyncArgs has no fields — sync runs across every registered repo.
type SyncArgs struct{}

// SyncResult mirrors the CLI's sync summary: how many branches were
// successfully refreshed and a per-branch error map for the rest.
type SyncResult struct {
	Synced int               `json:"synced"`
	Errors map[string]string `json:"errors"`
}

func (h *handlers) sync(ctx context.Context, _ *mcp.CallToolRequest, _ SyncArgs) (*mcp.CallToolResult, SyncResult, error) {
	res, err := h.workflow.Sync(ctx)
	if err != nil {
		return nil, SyncResult{}, err
	}
	out := SyncResult{Synced: res.Synced, Errors: make(map[string]string, len(res.Errors))}
	for k, e := range res.Errors {
		out.Errors[k] = e.Error()
	}
	return nil, out, nil
}

// ReconcileArgs has no fields — reconcile sweeps every registered repo.
type ReconcileArgs struct{}

// ReconcileResult is a plain ack so the agent can verify success.
type ReconcileResult struct {
	OK bool `json:"ok"`
}

func (h *handlers) reconcile(ctx context.Context, _ *mcp.CallToolRequest, _ ReconcileArgs) (*mcp.CallToolResult, ReconcileResult, error) {
	if err := h.workflow.Reconcile(ctx); err != nil {
		return toolError("reconcile: %v", err), ReconcileResult{}, nil
	}
	return nil, ReconcileResult{OK: true}, nil
}

// RegisterRepoArgs is the input for register_repo. Path is required —
// the MCP server can't infer a meaningful cwd from chat context.
type RegisterRepoArgs struct {
	Path string `json:"path" jsonschema:"absolute path to the repo on disk"`
	Name string `json:"name,omitempty" jsonschema:"defaults to the basename of path"`
}

func (h *handlers) registerRepo(ctx context.Context, _ *mcp.CallToolRequest, args RegisterRepoArgs) (*mcp.CallToolResult, *domain.Repo, error) {
	if args.Path == "" {
		return toolError("path is required (mcp has no cwd context)"), nil, nil
	}
	r, err := h.workflow.AddRepo(ctx, args.Path, args.Name)
	if err != nil {
		return toolError("register: %v", err), nil, nil
	}
	return nil, r, nil
}

// UnregisterRepoArgs is the input for unregister_repo.
type UnregisterRepoArgs struct {
	Name string `json:"name" jsonschema:"name of the registered repo to drop from tower's store"`
}

// UnregisterResult confirms the deletion.
type UnregisterResult struct {
	Unregistered bool   `json:"unregistered"`
	Name         string `json:"name"`
}

func (h *handlers) unregisterRepo(ctx context.Context, _ *mcp.CallToolRequest, args UnregisterRepoArgs) (*mcp.CallToolResult, UnregisterResult, error) {
	if args.Name == "" {
		return toolError("name is required"), UnregisterResult{}, nil
	}
	if err := h.workflow.RemoveRepo(ctx, args.Name); err != nil {
		return toolError("unregister: %v", err), UnregisterResult{}, nil
	}
	return nil, UnregisterResult{Unregistered: true, Name: args.Name}, nil
}

// PruneReposArgs is the input for prune_repos.
type PruneReposArgs struct {
	DryRun bool `json:"dry_run,omitempty" jsonschema:"when true, report which repos would be unregistered without actually removing them"`
}

// PruneReposResult lists which repos were removed (or would be in dry-run).
type PruneReposResult struct {
	Pruned []string `json:"pruned"`
	DryRun bool     `json:"dry_run"`
}

func (h *handlers) pruneRepos(ctx context.Context, _ *mcp.CallToolRequest, args PruneReposArgs) (*mcp.CallToolResult, PruneReposResult, error) {
	repos, err := h.store.ListRepos(ctx)
	if err != nil {
		return nil, PruneReposResult{}, err
	}
	missing := []string{}
	for _, r := range repos {
		if _, statErr := os.Stat(r.Path); errors.Is(statErr, fs.ErrNotExist) {
			missing = append(missing, r.Name)
		}
	}
	if args.DryRun {
		return nil, PruneReposResult{Pruned: missing, DryRun: true}, nil
	}
	for _, name := range missing {
		if err := h.workflow.RemoveRepo(ctx, name); err != nil {
			return toolError("remove %s: %v", name, err), PruneReposResult{}, nil
		}
	}
	return nil, PruneReposResult{Pruned: missing, DryRun: false}, nil
}

// ListReposArgs has no fields.
type ListReposArgs struct{}

// ListReposResult wraps the array because MCP tool outputs must be
// JSON objects, not bare arrays.
type ListReposResult struct {
	Repos []domain.Repo `json:"repos"`
}

func (h *handlers) listRepos(ctx context.Context, _ *mcp.CallToolRequest, _ ListReposArgs) (*mcp.CallToolResult, ListReposResult, error) {
	repos, err := h.store.ListRepos(ctx)
	if err != nil {
		return nil, ListReposResult{}, err
	}
	if repos == nil {
		repos = []domain.Repo{}
	}
	return nil, ListReposResult{Repos: repos}, nil
}

// buildView hydrates a worktree with its PR, reviews, and CI checks.
// Reviews/Checks are always non-nil so consumers don't need null checks.
func buildView(ctx context.Context, s store.Store, wt domain.Worktree) (domain.WorktreeView, error) {
	pr, err := s.GetPullRequest(ctx, wt.Repo, wt.Branch)
	if err != nil {
		return domain.WorktreeView{}, err
	}
	view := domain.WorktreeView{
		Worktree: wt,
		PR:       pr,
		Reviews:  []domain.Review{},
		Checks:   []domain.CICheck{},
	}
	if pr == nil {
		return view, nil
	}
	revs, err := s.ListReviews(ctx, wt.Repo, pr.Number)
	if err != nil {
		return domain.WorktreeView{}, err
	}
	if revs != nil {
		view.Reviews = revs
	}
	checks, err := s.ListCIChecks(ctx, wt.Repo, pr.Number)
	if err != nil {
		return domain.WorktreeView{}, err
	}
	if checks != nil {
		view.Checks = checks
	}
	return view, nil
}

// toolError builds a model-visible CallToolResult with IsError set.
// Use for user-facing errors (not found, validation); reserve protocol
// errors (returning err) for genuine system failures.
func toolError(format string, args ...any) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(format, args...)}},
	}
}
