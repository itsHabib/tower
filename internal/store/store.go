// Package store persists tower's view of tasks, worktrees, PRs, reviews,
// and CI checks.
package store

import (
	"context"

	"github.com/itsHabib/tower/internal/domain"
)

// Store is the persistence interface tower uses for all tracked state.
// All methods are safe to call concurrently from a single process.
type Store interface {
	UpsertTask(ctx context.Context, t domain.Task) error
	GetTask(ctx context.Context, id string) (*domain.Task, error)
	ListTasks(ctx context.Context) ([]domain.Task, error)
	DeleteTask(ctx context.Context, id string) error

	SetWorktree(ctx context.Context, wt domain.Worktree) error
	GetWorktree(ctx context.Context, taskID string) (*domain.Worktree, error)
	DeleteWorktree(ctx context.Context, taskID string) error

	SetPullRequest(ctx context.Context, pr domain.PullRequest) error
	GetPullRequest(ctx context.Context, taskID string) (*domain.PullRequest, error)

	UpsertReview(ctx context.Context, r domain.Review) error
	ListReviews(ctx context.Context, prNumber int) ([]domain.Review, error)

	UpsertCICheck(ctx context.Context, c domain.CICheck) error
	ListCIChecks(ctx context.Context, prNumber int) ([]domain.CICheck, error)

	Close() error
}
