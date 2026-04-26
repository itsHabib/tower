package mcp

import (
	"context"
	"fmt"

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
		Name:        "list_repos",
		Description: "List every registered repo (name + filesystem path).",
	}, h.listRepos)
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
