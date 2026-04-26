// Package refresh keeps the local store in sync with reality: live git
// worktrees and the GitHub state attached to each branch.
package refresh

import (
	"context"
	"fmt"
	"time"

	"github.com/itsHabib/tower/internal/domain"
	"github.com/itsHabib/tower/internal/observe"
	"github.com/itsHabib/tower/internal/store"
)

// Service syncs the store from git (worktrees) and gh (PR/review/CI).
type Service struct {
	Store store.Store
	Git   observe.Git
	GH    observe.GH
	now   func() time.Time
}

// New constructs a Service.
func New(s store.Store, git observe.Git, gh observe.GH) *Service {
	return &Service{
		Store: s, Git: git, GH: gh,
		now: func() time.Time { return time.Now().UTC() },
	}
}

// Reconcile syncs the worktree set from git into the store. Worktrees
// not in the live list are deleted; new ones are inserted with full
// per-worktree state (dirty, ahead/behind, last commit).
func (s *Service) Reconcile(ctx context.Context) error {
	live, err := s.Git.Worktrees(ctx)
	if err != nil {
		return fmt.Errorf("worktrees: %w", err)
	}
	seen := make(map[string]bool, len(live))
	for _, lw := range live {
		if lw.Branch == "" {
			continue
		}
		seen[lw.Branch] = true
		if err := s.upsertLive(ctx, lw); err != nil {
			return err
		}
	}
	stored, err := s.Store.ListWorktrees(ctx)
	if err != nil {
		return fmt.Errorf("list stored: %w", err)
	}
	for _, sw := range stored {
		if seen[sw.Branch] {
			continue
		}
		if err := s.Store.DeleteWorktree(ctx, sw.Branch); err != nil {
			return fmt.Errorf("delete stale %s: %w", sw.Branch, err)
		}
	}
	return nil
}

func (s *Service) upsertLive(ctx context.Context, lw observe.Worktree) error {
	existing, err := s.Store.GetWorktree(ctx, lw.Branch)
	if err != nil {
		return fmt.Errorf("get %s: %w", lw.Branch, err)
	}
	enriched := s.enrich(ctx, lw.Path)
	now := s.now()
	w := domain.Worktree{
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
func (s *Service) enrich(ctx context.Context, path string) enriched {
	var e enriched
	if d, err := s.Git.Dirty(ctx, path); err == nil {
		e.dirty = d
	}
	if a, b, err := s.Git.AheadBehind(ctx, path); err == nil {
		e.ahead, e.behind = a, b
	}
	if t, sub, err := s.Git.LastCommit(ctx, path); err == nil {
		e.lastCommit, e.title = t, sub
	}
	return e
}

// Branch fetches the PR, reviews, and CI checks for one branch and
// persists them. Returns nil if the branch has no PR yet.
func (s *Service) Branch(ctx context.Context, branch string) error {
	pr, err := s.GH.PullRequestForBranch(ctx, branch)
	if err != nil {
		return fmt.Errorf("pr for %s: %w", branch, err)
	}
	if pr == nil {
		return nil
	}
	pr.Branch = branch
	if err := s.Store.SetPullRequest(ctx, *pr); err != nil {
		return fmt.Errorf("set pr: %w", err)
	}
	reviews, err := s.GH.Reviews(ctx, pr.Number)
	if err != nil {
		return fmt.Errorf("reviews: %w", err)
	}
	for _, r := range reviews {
		if err := s.Store.UpsertReview(ctx, r); err != nil {
			return fmt.Errorf("upsert review: %w", err)
		}
	}
	checks, err := s.GH.Checks(ctx, pr.Number)
	if err != nil {
		return fmt.Errorf("checks: %w", err)
	}
	for _, c := range checks {
		if err := s.Store.UpsertCICheck(ctx, c); err != nil {
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

// All reconciles git state and then refreshes GitHub state for every
// known worktree. Per-branch errors land in the result, not the return.
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
		if err := s.Branch(ctx, w.Branch); err != nil {
			res.Errors[w.Branch] = err
			continue
		}
		res.Synced++
	}
	return res, nil
}
