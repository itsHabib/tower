// Package observe wraps the external tools tower talks to: git on the
// local filesystem and the gh CLI for GitHub state.
package observe

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"

	"github.com/itsHabib/tower/internal/domain"
)

// Worktree is a single entry from `git worktree list`.
type Worktree struct {
	Path   string
	Branch string
	HEAD   string
}

// Git is the local git surface tower uses: list, add, and remove worktrees.
type Git interface {
	Worktrees(ctx context.Context) ([]Worktree, error)
	AddWorktree(ctx context.Context, path, branch string) error
	RemoveWorktree(ctx context.Context, path string) error
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
