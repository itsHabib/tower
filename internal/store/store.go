// Package store persists tower's view of worktrees and the pull-request,
// review, and CI state attached to each.
package store

import (
	"context"

	"github.com/itsHabib/tower/internal/domain"
)

// Store is the persistence interface tower uses for all tracked state.
// All methods are safe to call concurrently from a single process.
type Store interface {
	UpsertWorktree(ctx context.Context, w domain.Worktree) error
	GetWorktree(ctx context.Context, branch string) (*domain.Worktree, error)
	ListWorktrees(ctx context.Context) ([]domain.Worktree, error)
	DeleteWorktree(ctx context.Context, branch string) error

	SetPullRequest(ctx context.Context, pr domain.PullRequest) error
	GetPullRequest(ctx context.Context, branch string) (*domain.PullRequest, error)

	UpsertReview(ctx context.Context, r domain.Review) error
	ListReviews(ctx context.Context, prNumber int) ([]domain.Review, error)

	UpsertCICheck(ctx context.Context, c domain.CICheck) error
	ListCIChecks(ctx context.Context, prNumber int) ([]domain.CICheck, error)

	Close() error
}
