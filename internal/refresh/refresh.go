// Package refresh keeps the local store in sync with reality across all
// registered repos: live git worktrees and the GitHub state attached to
// each branch.
package refresh

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/itsHabib/tower/internal/domain"
	"github.com/itsHabib/tower/internal/observe"
	"github.com/itsHabib/tower/internal/store"
)

// GitFactory returns a Git observer rooted at the given repo path.
type GitFactory func(repoPath string) observe.Git

// GHFactory returns a GH observer rooted at the given repo path.
type GHFactory func(repoPath string) observe.GH

// Service syncs the store from git (worktrees) and gh (PR/review/CI).
type Service struct {
	Store store.Store
	Git   GitFactory
	GH    GHFactory
	now   func() time.Time
}

// New constructs a Service.
func New(s store.Store, git GitFactory, gh GHFactory) *Service {
	return &Service{
		Store: s, Git: git, GH: gh,
		now: func() time.Time { return time.Now().UTC() },
	}
}

// Reconcile syncs the worktree set across every registered repo.
// Per-repo failures (typically a registered path that no longer
// exists on disk) are collected and returned together so one stale
// registration doesn't blank out the whole board.
func (s *Service) Reconcile(ctx context.Context) error {
	repos, err := s.Store.ListRepos(ctx)
	if err != nil {
		return fmt.Errorf("list repos: %w", err)
	}
	var failures []string
	for _, r := range repos {
		if err := s.ReconcileRepo(ctx, r); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", r.Name, err))
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("reconcile: %d repo(s) failed: %s. clean up with `tower repo prune`",
			len(failures), strings.Join(failures, "; "))
	}
	return nil
}

// ReconcileRepo syncs the worktree set for one repo.
func (s *Service) ReconcileRepo(ctx context.Context, repo domain.Repo) error {
	// Cheap up-front check: if the registered path is gone from
	// disk, skip cleanly with a typed error so the caller can decide
	// what to do (Reconcile aggregates these and points users at
	// `tower repo prune`).
	if _, err := os.Stat(repo.Path); errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("registered path missing on disk: %s", repo.Path)
	}
	git := s.Git(repo.Path)
	live, err := git.Worktrees(ctx)
	if err != nil {
		return fmt.Errorf("worktrees: %w", err)
	}
	seen := make(map[string]bool, len(live))
	for _, lw := range live {
		if lw.Branch == "" {
			continue
		}
		seen[lw.Branch] = true
		if err := s.upsertLive(ctx, repo, git, lw); err != nil {
			return err
		}
	}
	stored, err := s.Store.ListWorktreesForRepo(ctx, repo.Name)
	if err != nil {
		return fmt.Errorf("list stored: %w", err)
	}
	for _, sw := range stored {
		if seen[sw.Branch] {
			continue
		}
		if err := s.Store.DeleteWorktree(ctx, repo.Name, sw.Branch); err != nil {
			return fmt.Errorf("delete stale %s: %w", sw.Branch, err)
		}
	}
	return nil
}

func (s *Service) upsertLive(ctx context.Context, repo domain.Repo, git observe.Git, lw observe.Worktree) error {
	existing, err := s.Store.GetWorktree(ctx, repo.Name, lw.Branch)
	if err != nil {
		return fmt.Errorf("get %s/%s: %w", repo.Name, lw.Branch, err)
	}
	enriched := s.enrich(ctx, git, lw.Path)
	now := s.now()
	w := domain.Worktree{
		Repo:       repo.Name,
		Branch:     lw.Branch,
		Path:       lw.Path,
		HEAD:       lw.HEAD,
		Title:      enriched.title,
		Dirty:      enriched.dirty,
		Ahead:      enriched.ahead,
		Behind:     enriched.behind,
		LastCommit: enriched.lastCommit,
		LastSeen:   now,
	}
	if existing != nil {
		w.CreatedAt = existing.CreatedAt
	} else {
		w.CreatedAt = now
	}
	return s.Store.UpsertWorktree(ctx, w)
}

type enriched struct {
	dirty      bool
	ahead      int
	behind     int
	lastCommit time.Time
	title      string
}

// enrich gathers per-worktree state. Each call is best-effort; failures
// fall back to the zero value rather than aborting the whole reconcile.
func (s *Service) enrich(ctx context.Context, git observe.Git, path string) enriched {
	var e enriched
	if d, err := git.Dirty(ctx, path); err == nil {
		e.dirty = d
	}
	if a, b, err := git.AheadBehind(ctx, path); err == nil {
		e.ahead, e.behind = a, b
	}
	if t, sub, err := git.LastCommit(ctx, path); err == nil {
		e.lastCommit, e.title = t, sub
	}
	return e
}

// Branch fetches the PR, reviews, and CI checks for one branch in a
// repo and persists them. Returns nil if the branch has no PR yet.
func (s *Service) Branch(ctx context.Context, repoName, branch string) error {
	repo, err := s.Store.GetRepo(ctx, repoName)
	if err != nil {
		return fmt.Errorf("get repo: %w", err)
	}
	if repo == nil {
		return fmt.Errorf("repo not registered: %s", repoName)
	}
	gh := s.GH(repo.Path)
	pr, err := gh.PullRequestForBranch(ctx, branch)
	if err != nil {
		return fmt.Errorf("pr for %s/%s: %w", repoName, branch, err)
	}
	if pr == nil {
		return nil
	}
	pr.Repo = repoName
	pr.Branch = branch
	if err := s.Store.SetPullRequest(ctx, *pr); err != nil {
		return fmt.Errorf("set pr: %w", err)
	}
	reviews, err := gh.Reviews(ctx, pr.Number)
	if err != nil {
		return fmt.Errorf("reviews: %w", err)
	}
	for i := range reviews {
		reviews[i].Repo = repoName
		if err := s.Store.UpsertReview(ctx, reviews[i]); err != nil {
			return fmt.Errorf("upsert review: %w", err)
		}
	}
	checks, err := gh.Checks(ctx, pr.Number)
	if err != nil {
		return fmt.Errorf("checks: %w", err)
	}
	for i := range checks {
		checks[i].Repo = repoName
		if err := s.Store.UpsertCICheck(ctx, checks[i]); err != nil {
			return fmt.Errorf("upsert check: %w", err)
		}
	}
	return nil
}

// AllResult summarizes a full sync sweep.
type AllResult struct {
	Synced int
	Errors map[string]error
}

// All reconciles git state and refreshes GitHub state for every worktree
// across every registered repo. Per-branch errors land in the result.
func (s *Service) All(ctx context.Context) (AllResult, error) {
	if err := s.Reconcile(ctx); err != nil {
		return AllResult{}, fmt.Errorf("reconcile: %w", err)
	}
	worktrees, err := s.Store.ListWorktrees(ctx)
	if err != nil {
		return AllResult{}, fmt.Errorf("list worktrees: %w", err)
	}
	res := AllResult{Errors: make(map[string]error)}
	for _, w := range worktrees {
		key := w.Repo + "/" + w.Branch
		if err := s.Branch(ctx, w.Repo, w.Branch); err != nil {
			res.Errors[key] = err
			continue
		}
		res.Synced++
	}
	return res, nil
}
