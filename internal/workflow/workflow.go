// Package workflow composes the store, git ops, and refresh service into the
// high-level operations that the CLI and TUI both call.
package workflow

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/itsHabib/tower/internal/discover"
	"github.com/itsHabib/tower/internal/domain"
	"github.com/itsHabib/tower/internal/observe"
	"github.com/itsHabib/tower/internal/refresh"
	"github.com/itsHabib/tower/internal/store"
)

// Config controls where worktrees land and how branches are named.
type Config struct {
	Repo         string
	WorktreeBase string
	BranchPrefix string
}

func (c *Config) defaults() {
	if c.WorktreeBase == "" {
		c.WorktreeBase = ".worktrees"
	}
	if c.BranchPrefix == "" {
		c.BranchPrefix = "tower/"
	}
}

// Service is the unified workflow surface that callers (CLI, TUI) drive.
type Service struct {
	cfg     Config
	store   store.Store
	git     observe.Git
	refresh *refresh.Service
	now     func() time.Time
}

// New builds a Service. Empty Config fields fall back to sensible defaults
// (.worktrees/ for worktree base, tower/ branch prefix).
func New(cfg Config, s store.Store, git observe.Git, ref *refresh.Service) *Service {
	cfg.defaults()
	return &Service{cfg: cfg, store: s, git: git, refresh: ref, now: func() time.Time { return time.Now().UTC() }}
}

// Add creates a git worktree on a fresh branch for the named task and
// flips the task's status to active. Errors if the task is unknown or a
// worktree already exists.
func (s *Service) Add(ctx context.Context, taskID string) error {
	t, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("get task: %w", err)
	}
	if t == nil {
		return fmt.Errorf("task not found: %s", taskID)
	}
	existing, err := s.store.GetWorktree(ctx, taskID)
	if err != nil {
		return fmt.Errorf("get worktree: %w", err)
	}
	if existing != nil {
		return fmt.Errorf("worktree already exists for %s at %s", taskID, existing.Path)
	}

	wtPath := filepath.Join(s.cfg.Repo, s.cfg.WorktreeBase, t.ID)
	branch := s.cfg.BranchPrefix + t.ID

	if err := s.git.AddWorktree(ctx, wtPath, branch); err != nil {
		return fmt.Errorf("git add worktree: %w", err)
	}

	now := s.now()
	if err := s.store.SetWorktree(ctx, domain.Worktree{
		TaskID: t.ID, Path: wtPath, Branch: branch, CreatedAt: now,
	}); err != nil {
		return fmt.Errorf("set worktree: %w", err)
	}
	t.Status = domain.StatusActive
	t.UpdatedAt = now
	if err := s.store.UpsertTask(ctx, *t); err != nil {
		return fmt.Errorf("update task: %w", err)
	}
	return nil
}

// Remove tears down the task's worktree (if any) and marks the task abandoned.
func (s *Service) Remove(ctx context.Context, taskID string) error {
	wt, err := s.store.GetWorktree(ctx, taskID)
	if err != nil {
		return fmt.Errorf("get worktree: %w", err)
	}
	if wt != nil {
		if err := s.git.RemoveWorktree(ctx, wt.Path); err != nil {
			return fmt.Errorf("git remove worktree: %w", err)
		}
		if err := s.store.DeleteWorktree(ctx, taskID); err != nil {
			return fmt.Errorf("delete worktree: %w", err)
		}
	}
	t, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("get task: %w", err)
	}
	if t == nil {
		return nil
	}
	t.Status = domain.StatusAbandoned
	t.UpdatedAt = s.now()
	return s.store.UpsertTask(ctx, *t)
}

// Sync triggers a refresh sweep across every tracked task.
func (s *Service) Sync(ctx context.Context) (refresh.AllResult, error) {
	return s.refresh.All(ctx)
}

// DiscoverResult summarizes a Discover call.
type DiscoverResult struct {
	Added   int
	Updated int
	Tasks   []domain.Task
}

// Discover scans dir for markdown tasks and reconciles them with the store.
// New files become tasks; existing tasks have metadata refreshed but their
// status is preserved.
func (s *Service) Discover(ctx context.Context, dir string) (DiscoverResult, error) {
	found, err := discover.Scan(dir)
	if err != nil {
		return DiscoverResult{}, fmt.Errorf("scan: %w", err)
	}
	res := DiscoverResult{Tasks: found}
	for _, t := range found {
		existing, err := s.store.GetTask(ctx, t.ID)
		if err != nil {
			return res, fmt.Errorf("get task %s: %w", t.ID, err)
		}
		if existing != nil {
			t.Status = existing.Status
			t.CreatedAt = existing.CreatedAt
			t.UpdatedAt = s.now()
			if err := s.store.UpsertTask(ctx, t); err != nil {
				return res, fmt.Errorf("update task %s: %w", t.ID, err)
			}
			res.Updated++
			continue
		}
		if err := s.store.UpsertTask(ctx, t); err != nil {
			return res, fmt.Errorf("insert task %s: %w", t.ID, err)
		}
		res.Added++
	}
	return res, nil
}
