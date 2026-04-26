package observe

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"strings"
)

type GitObserver struct {
	Repo   string
	Runner Runner
}

func NewGit(repoRoot string) *GitObserver {
	return &GitObserver{Repo: repoRoot, Runner: ExecRunner{}}
}

func (g *GitObserver) Worktrees(ctx context.Context) ([]Worktree, error) {
	out, err := g.Runner.Run(ctx, g.Repo, "git", "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("git worktree list: %w", err)
	}
	return parseWorktreeList(out)
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
