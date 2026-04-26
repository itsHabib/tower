// Package workflow composes the store, git ops, and refresh service into
// the high-level operations that the CLI and TUI both call.
package workflow

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

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
	return &Service{
		cfg: cfg, store: s, git: git, refresh: ref,
		now: func() time.Time { return time.Now().UTC() },
	}
}

// Add creates a fresh worktree for the named work. Short names ("foo")
// land at <repo>/.worktrees/foo on branch tower/foo. A name containing a
// slash is treated as the full branch ref and the worktree path is
// derived from its last segment.
func (s *Service) Add(ctx context.Context, name string) (*domain.Worktree, error) {
	branch, slug := s.resolveBranchAndSlug(name)
	existing, err := s.store.GetWorktree(ctx, branch)
	if err != nil {
		return nil, fmt.Errorf("get worktree: %w", err)
	}
	if existing != nil {
		return nil, fmt.Errorf("worktree already tracked for branch %s at %s", branch, existing.Path)
	}
	wtPath := filepath.Join(s.cfg.Repo, s.cfg.WorktreeBase, slug)
	if err := s.git.AddWorktree(ctx, wtPath, branch); err != nil {
		return nil, fmt.Errorf("git add worktree: %w", err)
	}
	now := s.now()
	w := domain.Worktree{
		Branch: branch, Path: wtPath, CreatedAt: now, LastSeen: now,
	}
	if err := s.store.UpsertWorktree(ctx, w); err != nil {
		return nil, fmt.Errorf("upsert worktree: %w", err)
	}
	return &w, nil
}

// Remove tears down the worktree for the named branch (or short name).
func (s *Service) Remove(ctx context.Context, name string) error {
	branch := s.resolveBranch(name)
	wt, err := s.store.GetWorktree(ctx, branch)
	if err != nil {
		return fmt.Errorf("get worktree: %w", err)
	}
	if wt == nil {
		return fmt.Errorf("no worktree tracked for branch %s", branch)
	}
	if err := s.git.RemoveWorktree(ctx, wt.Path); err != nil {
		return fmt.Errorf("git remove worktree: %w", err)
	}
	return s.store.DeleteWorktree(ctx, branch)
}

// Sync triggers a full reconcile + GitHub refresh sweep.
func (s *Service) Sync(ctx context.Context) (refresh.AllResult, error) {
	return s.refresh.All(ctx)
}

// Reconcile pulls just the live git worktree state into the store. Cheap
// (no network); good for keeping the local view fresh.
func (s *Service) Reconcile(ctx context.Context) error {
	return s.refresh.Reconcile(ctx)
}

// Resolve returns the worktree for a name (short or full branch).
func (s *Service) Resolve(ctx context.Context, name string) (*domain.Worktree, error) {
	branch := s.resolveBranch(name)
	return s.store.GetWorktree(ctx, branch)
}

func (s *Service) resolveBranch(name string) string {
	if strings.Contains(name, "/") {
		return name
	}
	return s.cfg.BranchPrefix + name
}

func (s *Service) resolveBranchAndSlug(name string) (string, string) {
	if strings.Contains(name, "/") {
		parts := strings.Split(name, "/")
		return name, parts[len(parts)-1]
	}
	return s.cfg.BranchPrefix + name, name
}
