package observe

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"

	"github.com/itsHabib/tower/internal/domain"
)

type Worktree struct {
	Path   string
	Branch string
	HEAD   string
}

type Git interface {
	Worktrees(ctx context.Context) ([]Worktree, error)
	AddWorktree(ctx context.Context, path, branch string) error
	RemoveWorktree(ctx context.Context, path string) error
}

type GH interface {
	PullRequestForBranch(ctx context.Context, branch string) (*domain.PullRequest, error)
	Reviews(ctx context.Context, prNumber int) ([]domain.Review, error)
	Checks(ctx context.Context, prNumber int) ([]domain.CICheck, error)
}

type Runner interface {
	Run(ctx context.Context, dir, name string, args ...string) ([]byte, error)
}

type ExecRunner struct{}

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
