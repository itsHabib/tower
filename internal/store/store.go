// Package store persists tower's view of registered repos, the worktrees
// in each, and the pull-request / review / CI state attached.
package store

import (
	"context"

	"github.com/itsHabib/tower/internal/domain"
)

// Store is the persistence interface tower uses for all tracked state.
// All methods are safe to call concurrently from a single process.
type Store interface {
	UpsertRepo(ctx context.Context, r domain.Repo) error
	GetRepo(ctx context.Context, name string) (*domain.Repo, error)
	ListRepos(ctx context.Context) ([]domain.Repo, error)
	DeleteRepo(ctx context.Context, name string) error

	UpsertWorktree(ctx context.Context, w domain.Worktree) error
	GetWorktree(ctx context.Context, repo, branch string) (*domain.Worktree, error)
	ListWorktrees(ctx context.Context) ([]domain.Worktree, error)
	ListWorktreesForRepo(ctx context.Context, repo string) ([]domain.Worktree, error)
	DeleteWorktree(ctx context.Context, repo, branch string) error

	SetPullRequest(ctx context.Context, pr domain.PullRequest) error
	GetPullRequest(ctx context.Context, repo, branch string) (*domain.PullRequest, error)

	UpsertReview(ctx context.Context, r domain.Review) error
	ListReviews(ctx context.Context, repo string, prNumber int) ([]domain.Review, error)

	UpsertCICheck(ctx context.Context, c domain.CICheck) error
	ListCIChecks(ctx context.Context, repo string, prNumber int) ([]domain.CICheck, error)

	Close() error
}
