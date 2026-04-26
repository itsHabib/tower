// Package refresh pulls live state from external observers (gh) into the
// local store on demand.
package refresh

import (
	"context"
	"fmt"

	"github.com/itsHabib/tower/internal/observe"
	"github.com/itsHabib/tower/internal/store"
)

// Service syncs PR, review, and CI state for known tasks into the store.
type Service struct {
	Store store.Store
	GH    observe.GH
}

// New constructs a Service backed by store s and the GitHub observer gh.
func New(s store.Store, gh observe.GH) *Service {
	return &Service{Store: s, GH: gh}
}

// Task syncs the PR, reviews, and CI checks for a single tracked task.
// Returns nil if the task has no worktree or no PR yet.
func (s *Service) Task(ctx context.Context, taskID string) error {
	wt, err := s.Store.GetWorktree(ctx, taskID)
	if err != nil {
		return fmt.Errorf("get worktree: %w", err)
	}
	if wt == nil || wt.Branch == "" {
		return nil
	}

	pr, err := s.GH.PullRequestForBranch(ctx, wt.Branch)
	if err != nil {
		return fmt.Errorf("pr for branch %s: %w", wt.Branch, err)
	}
	if pr == nil {
		return nil
	}
	pr.TaskID = taskID
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

// AllResult summarizes a sweep across all tasks.
type AllResult struct {
	Synced int
	Errors map[string]error
}

// All sweeps every known task, syncing each independently. A failure on one
// task does not stop the sweep; per-task errors land in AllResult.Errors.
func (s *Service) All(ctx context.Context) (AllResult, error) {
	tasks, err := s.Store.ListTasks(ctx)
	if err != nil {
		return AllResult{}, fmt.Errorf("list tasks: %w", err)
	}
	res := AllResult{Errors: make(map[string]error)}
	for _, t := range tasks {
		if err := s.Task(ctx, t.ID); err != nil {
			res.Errors[t.ID] = err
			continue
		}
		res.Synced++
	}
	return res, nil
}
