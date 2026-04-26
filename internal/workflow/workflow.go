// Package workflow composes the store, git ops, and refresh service into
// the high-level operations that the CLI and TUI both call across all
// registered repos.
package workflow

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/itsHabib/tower/internal/domain"
	"github.com/itsHabib/tower/internal/refresh"
	"github.com/itsHabib/tower/internal/store"
)

// Config controls where worktrees land and how branches are named.
// Defaults: WorktreeBase=".worktrees", BranchPrefix="tower/".
type Config struct {
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

// Service is the unified workflow surface across all registered repos.
type Service struct {
	cfg     Config
	store   store.Store
	git     refresh.GitFactory
	refresh *refresh.Service
	now     func() time.Time
}

// New builds a Service.
func New(cfg Config, s store.Store, git refresh.GitFactory, ref *refresh.Service) *Service {
	cfg.defaults()
	return &Service{
		cfg: cfg, store: s, git: git, refresh: ref,
		now: func() time.Time { return time.Now().UTC() },
	}
}

// AddRepo registers a repo by path. Name defaults to the directory's basename.
func (s *Service) AddRepo(ctx context.Context, path, name string) (*domain.Repo, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("abs path: %w", err)
	}
	if name == "" {
		name = filepath.Base(abs)
	}
	existing, err := s.store.GetRepo(ctx, name)
	if err != nil {
		return nil, err
	}
	if existing != nil && existing.Path != abs {
		return nil, fmt.Errorf("repo name %q already registered at a different path: %s", name, existing.Path)
	}
	r := domain.Repo{Name: name, Path: abs, CreatedAt: s.now()}
	if existing != nil {
		r.CreatedAt = existing.CreatedAt
	}
	if err := s.store.UpsertRepo(ctx, r); err != nil {
		return nil, err
	}
	return &r, nil
}

// RemoveRepo unregisters a repo. Worktrees and PR state cascade away.
func (s *Service) RemoveRepo(ctx context.Context, name string) error {
	repo, err := s.store.GetRepo(ctx, name)
	if err != nil {
		return err
	}
	if repo == nil {
		return fmt.Errorf("repo not registered: %s", name)
	}
	return s.store.DeleteRepo(ctx, name)
}

// ListRepos returns every registered repo.
func (s *Service) ListRepos(ctx context.Context) ([]domain.Repo, error) {
	return s.store.ListRepos(ctx)
}

// Add creates a fresh worktree in the named repo.
func (s *Service) Add(ctx context.Context, repoName, name string) (*domain.Worktree, error) {
	repo, err := s.requireRepo(ctx, repoName)
	if err != nil {
		return nil, err
	}
	branch, slug := s.resolveBranchAndSlug(name)
	existing, err := s.store.GetWorktree(ctx, repo.Name, branch)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, fmt.Errorf("worktree already tracked for %s/%s at %s", repo.Name, branch, existing.Path)
	}
	wtPath := filepath.Join(repo.Path, s.cfg.WorktreeBase, slug)
	if err := s.git(repo.Path).AddWorktree(ctx, wtPath, branch); err != nil {
		return nil, fmt.Errorf("git add worktree: %w", err)
	}
	now := s.now()
	w := domain.Worktree{
		Repo: repo.Name, Branch: branch, Path: wtPath,
		CreatedAt: now, LastSeen: now,
	}
	if err := s.store.UpsertWorktree(ctx, w); err != nil {
		return nil, err
	}
	return &w, nil
}

// Remove tears down the worktree on the named branch in the named repo.
func (s *Service) Remove(ctx context.Context, repoName, name string) error {
	repo, err := s.requireRepo(ctx, repoName)
	if err != nil {
		return err
	}
	branch := s.resolveBranch(name)
	wt, err := s.store.GetWorktree(ctx, repo.Name, branch)
	if err != nil {
		return err
	}
	if wt == nil {
		return fmt.Errorf("no worktree tracked for %s/%s", repo.Name, branch)
	}
	if err := s.git(repo.Path).RemoveWorktree(ctx, wt.Path); err != nil {
		return fmt.Errorf("git remove worktree: %w", err)
	}
	return s.store.DeleteWorktree(ctx, repo.Name, branch)
}

// Sync triggers a full reconcile + GitHub refresh sweep across all repos.
func (s *Service) Sync(ctx context.Context) (refresh.AllResult, error) {
	return s.refresh.All(ctx)
}

// Reconcile pulls just the live git worktree state into the store across
// all registered repos. Cheap; no network.
func (s *Service) Reconcile(ctx context.Context) error {
	return s.refresh.Reconcile(ctx)
}

// ListWorktrees returns every worktree across all registered repos.
func (s *Service) ListWorktrees(ctx context.Context) ([]domain.Worktree, error) {
	return s.store.ListWorktrees(ctx)
}

// ResolveResult describes the outcome of resolving a worktree by short name.
type ResolveResult struct {
	Worktree *domain.Worktree
	Matches  []domain.Worktree // populated only when ambiguous
}

// ErrAmbiguous is returned when a name matches in more than one repo and
// no repo was specified to disambiguate.
var ErrAmbiguous = errors.New("name matches in multiple repos; specify --repo")

// Resolve finds a worktree by name. If repoName is empty, searches all
// repos and errors with ErrAmbiguous if more than one matches.
func (s *Service) Resolve(ctx context.Context, repoName, name string) (*domain.Worktree, error) {
	branch := s.resolveBranch(name)
	if repoName != "" {
		return s.store.GetWorktree(ctx, repoName, branch)
	}
	all, err := s.store.ListWorktrees(ctx)
	if err != nil {
		return nil, err
	}
	var matches []domain.Worktree
	for _, w := range all {
		if w.Branch == branch {
			matches = append(matches, w)
		}
	}
	switch len(matches) {
	case 0:
		return nil, nil
	case 1:
		return &matches[0], nil
	default:
		return nil, fmt.Errorf("%w: %s found in %d repos", ErrAmbiguous, branch, len(matches))
	}
}

// RepoForPath finds the registered repo containing the given absolute path
// (matches by path prefix).
func (s *Service) RepoForPath(ctx context.Context, path string) (*domain.Repo, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	repos, err := s.store.ListRepos(ctx)
	if err != nil {
		return nil, err
	}
	abs = filepath.Clean(abs)
	for i := range repos {
		if abs == repos[i].Path || strings.HasPrefix(abs, repos[i].Path+string(filepath.Separator)) {
			return &repos[i], nil
		}
	}
	return nil, nil
}

func (s *Service) requireRepo(ctx context.Context, name string) (*domain.Repo, error) {
	if name == "" {
		return nil, errors.New("repo name required")
	}
	repo, err := s.store.GetRepo(ctx, name)
	if err != nil {
		return nil, err
	}
	if repo == nil {
		return nil, fmt.Errorf("repo not registered: %s (run `tower repo add` from the repo)", name)
	}
	return repo, nil
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
