package observe

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"strings"
)

// GitObserver implements Git by shelling out to the local git binary.
type GitObserver struct {
	Repo   string
	Runner Runner
}

// NewGit returns a GitObserver rooted at repoRoot, using ExecRunner.
func NewGit(repoRoot string) *GitObserver {
	return &GitObserver{Repo: repoRoot, Runner: ExecRunner{}}
}

// Worktrees lists every worktree attached to the repository.
func (g *GitObserver) Worktrees(ctx context.Context) ([]Worktree, error) {
	out, err := g.Runner.Run(ctx, g.Repo, "git", "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("git worktree list: %w", err)
	}
	return parseWorktreeList(out)
}

// AddWorktree creates a new worktree at path on a fresh branch.
func (g *GitObserver) AddWorktree(ctx context.Context, path, branch string) error {
	if _, err := g.Runner.Run(ctx, g.Repo, "git", "worktree", "add", "-b", branch, path); err != nil {
		return fmt.Errorf("git worktree add: %w", err)
	}
	return nil
}

// RemoveWorktree tears down the worktree at path.
func (g *GitObserver) RemoveWorktree(ctx context.Context, path string) error {
	if _, err := g.Runner.Run(ctx, g.Repo, "git", "worktree", "remove", path); err != nil {
		return fmt.Errorf("git worktree remove: %w", err)
	}
	return nil
}

func parseWorktreeList(data []byte) ([]Worktree, error) {
	var out []Worktree
	var cur Worktree
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			if cur.Path != "" {
				out = append(out, cur)
			}
			cur = Worktree{}
			continue
		}
		switch {
		case strings.HasPrefix(line, "worktree "):
			cur.Path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "HEAD "):
			cur.HEAD = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		}
	}
	if cur.Path != "" {
		out = append(out, cur)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
