// Package observe wraps the external tools tower talks to: git on the
// local filesystem and the gh CLI for GitHub state.
package observe

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/itsHabib/tower/internal/domain"
)

// Worktree is a single entry from `git worktree list`.
type Worktree struct {
	Path   string
	Branch string
	HEAD   string
}

// Git is the local git surface tower uses.
type Git interface {
	Worktrees(ctx context.Context) ([]Worktree, error)
	AddWorktree(ctx context.Context, path, branch string) error
	// RemoveWorktree removes the worktree at path. force=true passes
	// --force to git, discarding uncommitted changes; without it git
	// refuses on a dirty worktree.
	RemoveWorktree(ctx context.Context, path string, force bool) error
	// DeleteBranch deletes the named branch only if it is fully merged
	// into its upstream (or HEAD). Refuses with an error otherwise so
	// unmerged commits aren't silently discarded — callers should
	// surface that as a warning and let the user force-delete by hand
	// if they actually want to throw the work away.
	DeleteBranch(ctx context.Context, branch string) error
	Dirty(ctx context.Context, path string) (bool, error)
	// AheadBehind returns commits ahead and commits behind the worktree's
	// upstream (in that order). Returns (0, 0, nil) when no upstream is set.
	AheadBehind(ctx context.Context, path string) (int, int, error)
	// LastCommit returns HEAD's timestamp and subject for the worktree at path.
	LastCommit(ctx context.Context, path string) (time.Time, string, error)
	// MainRoot returns the absolute path of the main worktree of the repo.
	MainRoot(ctx context.Context) (string, error)
}

// GH is the GitHub surface tower uses to read PR, review, and CI state.
type GH interface {
	PullRequestForBranch(ctx context.Context, branch string) (*domain.PullRequest, error)
	Reviews(ctx context.Context, prNumber int) ([]domain.Review, error)
	Checks(ctx context.Context, prNumber int) ([]domain.CICheck, error)
}

// Runner executes an external command and returns its stdout.
// It is the seam tests use to substitute fake command output.
type Runner interface {
	Run(ctx context.Context, dir, name string, args ...string) ([]byte, error)
}

// ExecRunner is the production Runner that shells out via os/exec.
type ExecRunner struct{}

// Run shells out to name with args, optionally in dir, and returns stdout.
// On non-zero exit, the returned error includes captured stderr.
func (ExecRunner) Run(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return nil, fmt.Errorf("%s %v: %w (stderr: %s)", name, args, err, bytes.TrimSpace(stderr.Bytes()))
		}
		return nil, fmt.Errorf("%s %v: %w", name, args, err)
	}
	return out, nil
}
